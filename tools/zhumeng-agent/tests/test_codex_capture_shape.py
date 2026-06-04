import json
from pathlib import Path

from zhumeng_agent.adapters.codex.capture_redact import classify_source, content_policy_for_class
from zhumeng_agent.adapters.codex.capture_shape import (
    build_spawn_agent_model_override_report,
    build_subagent_registration_report,
    capture_model_picker_state,
    shape_app_server_frame,
    shape_subagent_registration_event,
    shape_tool_lifecycle_event,
    tee_frames_without_mutation,
)
from zhumeng_agent.adapters.codex.capture_writer import BoundedCaptureQueue, JsonlTraceWriter, read_jsonl
from zhumeng_agent.adapters.codex.capture_config import CorrelationHasher


def _repo_root_from_test_file() -> Path:
    for parent in Path(__file__).resolve().parents:
        if (parent / "backend/internal/service/testdata").is_dir():
            return parent
    raise AssertionError("could not locate repository root from test file")


def test_source_classification_allows_raw_only_for_builtin_sources():
    assert content_policy_for_class(classify_source("codex_open_source")) == "raw_allowed"
    assert content_policy_for_class(classify_source("codex_builtin")) == "raw_allowed"
    assert content_policy_for_class(classify_source("desktop_bundled_builtin")) == "raw_allowed"

    for source in ["user_config", "project_doc", "tool_output", "browser_content", "screenshot", "unknown"]:
        assert content_policy_for_class(classify_source(source)) == "shape_only"


def test_app_server_frame_shape_excludes_raw_payload():
    frame = b'{"id":1,"method":"model/list","params":{"prompt":"SECRET_PROMPT"}}'

    event = shape_app_server_frame(frame, direction="desktop_to_app_server", hasher=CorrelationHasher.from_key_file(None))

    assert event["method"] == "model/list"
    assert event["id_present"] is True
    assert event["payload_policy"] == "shape_only"
    assert event["payload_hash"].startswith("sha256:")
    assert "SECRET_PROMPT" not in json.dumps(event)
    assert event["payload_shape"]["params"]["prompt"] == "str"


def test_app_server_frame_shape_redacts_sensitive_json_keys():
    frame = json.dumps({
        "/Users/alice/private/repo/file.py": "value",
        "https://github.com/org/private": "value",
        "feature/private-branch": "value",
        "safeField": "value",
    }).encode("utf-8")

    event = shape_app_server_frame(frame, direction="desktop_to_app_server")
    dumped = json.dumps(event)

    assert "/Users/alice" not in dumped
    assert "github.com/org/private" not in dumped
    assert "feature/private-branch" not in dumped
    assert "safeField" in dumped
    assert any(key.startswith("field_hash_") for key in event["payload_shape"])


def test_app_server_frame_shape_redacts_sensitive_header_keys():
    frame = json.dumps({
        "Cookie": "secret",
        "Authorization": "Bearer sk-test",
        "x-api-key": "secret",
    }).encode("utf-8")

    event = shape_app_server_frame(frame, direction="desktop_to_app_server")
    dumped = json.dumps(event)

    assert "Cookie" not in dumped
    assert "Authorization" not in dumped
    assert "x-api-key" not in dumped
    assert len(event["payload_shape"]) == 3
    assert all(key.startswith("field_hash_") for key in event["payload_shape"])


def test_app_server_frame_shape_can_attach_correlation_hashes_with_shared_key(tmp_path):
    key = tmp_path / "key"
    key.write_text("shared", encoding="utf-8")
    event = shape_app_server_frame(
        b'{"id":1,"method":"turn/start","params":{}}',
        direction="desktop_to_app_server",
        hasher=CorrelationHasher.from_key_file(key),
        correlation_ids={"x_client_request_id": "request-1", "thread_id": "thread-1"},
        model="deepseek-v4-pro",
        request_path="/codex/v1/responses",
    )

    assert event["correlation_hashes"]["x_client_request_id_hash"].startswith("hmac-sha256:")
    assert event["model"] == "deepseek-v4-pro"
    assert event["request_path"] == "/codex/v1/responses"
    assert "request-1" not in json.dumps(event)


def test_app_server_frame_shape_handles_malformed_and_binary_payloads():
    malformed = shape_app_server_frame(b'{"id":', direction="desktop_to_app_server")
    binary = shape_app_server_frame(b"\x00\xff\x01", direction="app_server_to_desktop")

    assert malformed["malformed"] is True
    assert malformed["payload_policy"] == "hash_only"
    assert binary["payload_bytes"] == 3
    assert binary["payload_policy"] == "hash_only"


def test_subagent_registration_event_shape_hashes_ids_and_classifies_console_messages():
    event = shape_subagent_registration_event(
        event_name="console",
        conversation_id="conversation-secret",
        thread_id="thread-secret",
        status="unknown conversation: conversation-secret",
        message="unknown conversation: conversation-secret raw prompt must not leak",
        ts="2026-06-03T11:48:43.000Z",
        hasher=CorrelationHasher.from_key_file(None),
    )

    dumped = json.dumps(event)
    assert event["event_type"] == "subagent_registration"
    assert event["event_name"] == "console"
    assert event["conversation_id_hash"].startswith("sha256:")
    assert event["thread_id_hash"].startswith("sha256:")
    assert event["message_class"] == "unknown_conversation"
    assert "conversation-secret" not in dumped
    assert "thread-secret" not in dumped
    assert "raw prompt" not in dumped


def test_subagent_registration_report_flags_unknown_before_recovery():
    hasher = CorrelationHasher.from_key_file(None)
    events = [
        shape_subagent_registration_event(event_name="item/started", conversation_id="c1", thread_id="t1", ts="2026-06-03T11:48:40.000Z", hasher=hasher),
        shape_subagent_registration_event(event_name="console", conversation_id="c1", thread_id="t1", message="unknown conversation c1", ts="2026-06-03T11:48:41.000Z", hasher=hasher),
        shape_subagent_registration_event(event_name="thread/start", conversation_id="c1", thread_id="t1", ts="2026-06-03T11:48:42.000Z", hasher=hasher),
        shape_subagent_registration_event(event_name="console", conversation_id="c1", thread_id="t1", message="maybe_resume_success", ts="2026-06-03T11:48:43.000Z", hasher=hasher),
    ]

    report = build_subagent_registration_report(events)

    assert report["subagent_registration_race_suspected"] is True
    assert report["first_item_before_conversation_registered"] is True
    assert report["unknown_conversation_count"] == 1
    assert report["maybe_resume_success_after_unknown_conversation"] is True
    assert report["thread_read_empty_count"] == 0


def test_subagent_registration_report_tracks_registration_per_conversation_group():
    hasher = CorrelationHasher.from_key_file(None)
    events = [
        shape_subagent_registration_event(event_name="thread/start", conversation_id="registered", thread_id="t1", ts="2026-06-03T11:48:40.000Z", hasher=hasher),
        shape_subagent_registration_event(event_name="item/started", conversation_id="registered", thread_id="t1", ts="2026-06-03T11:48:41.000Z", hasher=hasher),
        shape_subagent_registration_event(event_name="item/started", conversation_id="unregistered", thread_id="t2", ts="2026-06-03T11:48:42.000Z", hasher=hasher),
    ]

    report = build_subagent_registration_report(events)

    assert report["first_item_before_conversation_registered"] is True
    assert report["subagent_registration_race_suspected"] is True
    assert report["subagent_registration_order"][0]["event_name"] == "thread/start"
    assert report["subagent_registration_order"][0]["conversation_id_hash"].startswith("sha256:")
    assert report["subagent_registration_order"][2]["event_name"] == "item/started"
    assert report["subagent_registration_order"][2]["thread_id_hash"].startswith("sha256:")


def test_tee_frames_preserves_bytes_and_order_when_writer_fails():
    frames = [b'{"id":1}', b'{"id":2}']

    def failing_writer(event):
        raise OSError("disk full")

    assert tee_frames_without_mutation(frames, writer=failing_writer) == frames


def test_capture_writer_failure_and_queue_overflow_are_non_fatal(tmp_path):
    writer = JsonlTraceWriter(tmp_path / "missing" / "events.jsonl")
    assert writer.safe_write({"event": "ok"}) is True

    queue = BoundedCaptureQueue(max_size=1)
    assert queue.push({"seq": 1}) is True
    assert queue.push({"seq": 2}) is False
    assert queue.items == [{"seq": 1}]
    assert queue.dropped == 1


def test_tool_lifecycle_shape_only_policy_for_sensitive_outputs():
    sentinel = "RAW_BROWSER_TEXT_AND_COOKIE_Bearer abc"
    event = shape_tool_lifecycle_event(
        tool_name="mcp__browser__read_page",
        call_id="call-secret",
        item_id="item-secret",
        schema={"type": "object", "properties": {"url": {"type": "string"}}},
        result=sentinel,
        content_class="browser_content",
        status="completed",
        duration_ms=12,
        sent_back_to_model=True,
        hasher=CorrelationHasher.from_key_file(None),
    )

    dumped = json.dumps(event)
    assert sentinel not in dumped
    assert "call-secret" not in dumped
    assert event["content_class"] == "browser_content"
    assert event["policy_decision"] == "shape_only"
    assert event["redaction_reason"] == "default_no_user_content"
    assert event["result_chars"] == len(sentinel)
    assert event["result_hash"].startswith("sha256:")
    assert event["schema_hash"].startswith("sha256:")
    assert event["sent_back_to_model"] is True


def test_tool_lifecycle_can_record_ui_matrix_and_trace_correlation(tmp_path):
    key = tmp_path / "key"
    key.write_bytes(b"shared")
    event = shape_tool_lifecycle_event(
        tool_name="shell_exec",
        call_id="call-secret",
        item_id="item-secret",
        schema={"type": "object"},
        result="ok",
        content_class="command_output",
        status="completed",
        duration_ms=5,
        sent_back_to_model=True,
        hasher=CorrelationHasher.from_key_file(key),
        desktop_trace_id="cd_runtime",
        correlation_ids={"x_client_request_id": "request-1"},
        model="deepseek-v4-pro",
        request_path="/codex/v1/responses",
        ui_matrix={
            "command_collapsed": True,
            "command_expandable": True,
            "tool_detail_expandable": False,
            "diff_entry_visible": True,
            "file_open_action_available": False,
        },
        degraded_reason="diff_not_materialized",
        pass_fail_rule="diff_entry_visible implies replayable artifact metadata",
    )

    assert event["ui_matrix"]["command_collapsed"] is True
    assert event["ui_matrix"]["tool_detail_expandable"] is False
    assert event["degraded_reason"] == "diff_not_materialized"
    assert event["pass_fail_rule"] == "diff_entry_visible implies replayable artifact metadata"
    assert event["desktop_trace_id"] == "cd_runtime"
    assert event["correlation_hashes"]["x_client_request_id_hash"].startswith("hmac-sha256:")
    assert event["trace_correlation"]["strategy"] == "shared_hash"
    assert event["trace_correlation"]["link_ready"] is True


def test_tool_lifecycle_redacts_all_sensitive_tool_families(tmp_path):
    cases = [
        ("mcp__computer_use__screenshot", "screenshot", "PNG_SCREENSHOT_BYTES"),
        ("mcp__browser__read_page", "browser_content", "RAW_BROWSER_PAGE_TEXT"),
        ("mcp__chrome__read_page", "browser_content", "RAW_CHROME_PAGE_TEXT Cookie=x"),
        ("mcp__exa__web_search_exa", "tool_output", "RAW_MCP_TEXT"),
        ("shell_exec", "command_output", "RAW_COMMAND_OUTPUT"),
        ("plugin_json_tool", "json_metadata", '{"secret":"RAW_PLUGIN_JSON"}'),
        ("file_read", "file_content", "RAW_FILE_CONTENT"),
    ]
    writer = JsonlTraceWriter(tmp_path / "tool_lifecycle.jsonl")
    for tool_name, content_class, sentinel in cases:
        writer.write(shape_tool_lifecycle_event(
            tool_name=tool_name,
            call_id=f"{tool_name}-call",
            item_id=f"{tool_name}-item",
            schema={"tool": tool_name},
            result=sentinel,
            content_class=content_class,
            status="completed",
            duration_ms=1,
            sent_back_to_model=True,
            hasher=CorrelationHasher.from_key_file(None),
        ))

    text = (tmp_path / "tool_lifecycle.jsonl").read_text(encoding="utf-8")
    for _, _, sentinel in cases:
        assert sentinel not in text
    events = read_jsonl(tmp_path / "tool_lifecycle.jsonl")
    assert {event["content_class"] for event in events} == {case[1] for case in cases}
    assert all(event["policy_decision"] == "shape_only" for event in events)
    assert all(event["redaction_reason"] == "default_no_user_content" for event in events)


def test_model_picker_capture_records_catalog_state_without_patching():
    event = capture_model_picker_state(
        app_server_models=[
            {"model": "deepseek-v4-pro", "displayName": "DeepSeek V4 Pro", "hidden": False, "supportedReasoningEfforts": ["low", "high"], "defaultReasoningEffort": "high"},
            {"model": "gpt-hidden", "displayName": "GPT Hidden", "hidden": True},
        ],
        selected_model="deepseek-v4-pro",
        selected_reasoning_effort="high",
        ui_visible_model_ids=["deepseek-v4-pro"],
        model_picker_patch_state={"status": "unpatched"},
    )

    assert event["app_server_model_ids"] == ["deepseek-v4-pro", "gpt-hidden"]
    assert event["ui_visible_model_ids"] == ["deepseek-v4-pro"]
    assert event["model_picker_patch_state"]["status"] == "unpatched"
    assert event["capture_modified_model_visibility"] is False


def test_model_picker_capture_sanitizes_patch_state_paths():
    event = capture_model_picker_state(
        app_server_models=[],
        selected_model=None,
        selected_reasoning_effort=None,
        ui_visible_model_ids=[],
        model_picker_patch_state={
            "status": "patched",
            "app_path": "/Applications/Codex.app",
            "backup_path": "/Users/alice/backups/app.asar",
            "target_file": "webview/assets/model-queries-test.js",
        },
    )

    dumped = json.dumps(event)
    assert "/Applications" not in dumped
    assert "/Users/alice" not in dumped
    assert event["model_picker_patch_state"]["app_path_hash"].startswith("sha256:")
    assert event["model_picker_patch_state"]["backup_path_hash"].startswith("sha256:")
    assert event["model_picker_patch_state"]["target_file"] == "webview/assets/model-queries-test.js"


def test_model_picker_capture_sanitizes_patch_state_repo_branch_and_commit():
    event = capture_model_picker_state(
        app_server_models=[],
        selected_model=None,
        selected_reasoning_effort=None,
        ui_visible_model_ids=[],
        model_picker_patch_state={
            "status": "patched",
            "repo_url": "https://github.com/org/private",
            "branch": "feature/private-branch",
            "commit": "0123456789abcdef0123456789abcdef01234567",
            "revision": "refs/heads/main",
        },
    )

    dumped = json.dumps(event)
    assert "github.com/org/private" not in dumped
    assert "feature/private-branch" not in dumped
    assert "0123456789abcdef0123456789abcdef01234567" not in dumped
    assert "refs/heads/main" not in dumped
    assert event["model_picker_patch_state"]["repo_url_hash"].startswith("sha256:")
    assert event["model_picker_patch_state"]["branch_hash"].startswith("sha256:")
    assert event["model_picker_patch_state"]["commit_hash"].startswith("sha256:")
    assert event["model_picker_patch_state"]["revision_hash"].startswith("sha256:")


def test_deepseek_native_parity_fixtures_preserve_sanitized_capture_shape():
    fixture_dir = (
        _repo_root_from_test_file()
        / "backend/internal/service/testdata/codex_gateway_deepseek_native_parity"
    )
    native = json.loads((fixture_dir / "native_tool_search_call_output.json").read_text(encoding="utf-8"))
    failed = json.loads((fixture_dir / "failed_tool_search_function_call.json").read_text(encoding="utf-8"))
    computer_sizes = json.loads((fixture_dir / "computer_use_output_sizes.json").read_text(encoding="utf-8"))

    assert native["source_baseline"] == "successful_codex_native_deepseek_bridge"
    assert failed["source_baseline"] == "observed_deepseek_failure"
    assert computer_sizes["source_baseline"] == "successful_codex_native_deepseek_bridge_child_session"

    tool_search_call = native["tool_search_call"]
    tool_search_output = native["tool_search_output"]
    assert tool_search_call == {
        "type": "tool_search_call",
        "call_id": "call_fixture",
        "status": "completed",
        "execution": "client",
        "arguments": {
            "query": "sub-agent dispatch multi-agent DeepSeek V4 Flash model tool",
            "limit": 10,
        },
    }
    assert tool_search_output["type"] == "tool_search_output"
    assert tool_search_output["call_id"] == tool_search_call["call_id"]
    assert tool_search_output["status"] == "completed"
    assert tool_search_output["execution"] == "client"

    namespaces = {
        namespace["name"]: namespace
        for namespace in tool_search_output["tools"]
        if namespace["type"] == "namespace"
    }
    multi_agent_tools = {
        tool["name"]: tool
        for tool in namespaces["multi_agent_v1"]["tools"]
    }
    spawn_agent = multi_agent_tools["spawn_agent"]
    assert spawn_agent["description"] == "sanitized spawn-agent tool description"
    assert spawn_agent["input_schema"]["properties"]["task"]["type"] == "string"
    assert spawn_agent["input_schema"]["required"] == ["task"]

    assert failed["item"]["type"] == "function_call"
    assert failed["item"]["name"] == "tool_search"
    assert failed["item"]["matching_tool_search_output_present"] is False

    assert computer_sizes["fixture_label"] == "computer_use_output_size_visibility"
    for sample in computer_sizes["samples"]:
        assert sample["app_state_close_marker_present"] is True
        assert sample["deepseek_visible_normalized_output_retained_computer_screenshot"] is True
        assert sample["deepseek_visible_normalized_output_retained_operable_lines"] is True
        assert sample["deepseek_visible_normalized_output_retained_lower_screen_actionable_lines"] is True
        assert isinstance(sample["raw_output_chars"], int)
        assert isinstance(sample["app_state_chars"], int)
        assert isinstance(sample["screenshot_chars"], int)


def test_spawn_agent_model_override_report_detects_deepseek_mismatch_without_raw_content():
    report = build_spawn_agent_model_override_report(
        events=[{
            "capture_ts": "2026-06-03T11:43:33.599Z",
            "tools": [{
                "type": "namespace",
                "name": "multi_agent_v1",
                "tools": [{
                    "name": "spawn_agent",
                    "description": "sanitized spawn tool",
                    "input_schema": {
                        "type": "object",
                        "properties": {"model": {"type": "string", "enum": ["claude-sonnet-4-6"]}},
                    },
                }],
            }],
        }],
        catalog_models=["deepseek-v4-pro", "deepseek-v4-flash", "claude-sonnet-4-6"],
        catalog_hash="abc123",
        catalog_mtime="2026-06-03T11:40:00Z",
    )

    assert report["spawn_agent_model_override_mismatch"] is True
    assert report["catalog_has_deepseek"] is True
    assert report["spawn_agent_has_deepseek"] is False
    assert report["capture_ts"] == "2026-06-03T11:43:33.599Z"
    assert "sanitized spawn tool" not in json.dumps(report)


def test_spawn_agent_model_override_report_flags_missing_model_override_list():
    report = build_spawn_agent_model_override_report(
        events=[{
            "capture_ts": "2026-06-03T11:43:33.599Z",
            "tools": [{"name": "spawn_agent", "input_schema": {"type": "object", "properties": {"task": {"type": "string"}}}}],
        }],
        catalog_models=["deepseek-v4-pro", "claude-sonnet-4-6"],
        catalog_hash="abc123",
        catalog_mtime="2026-06-03T11:40:00Z",
    )

    assert report["spawn_agent_model_override_mismatch"] is True
    assert report["catalog_has_deepseek"] is True
    assert report["spawn_agent_has_deepseek"] is False
    assert report["spawn_agent_present"] is True
    assert report["spawn_agent_model_count"] == 0

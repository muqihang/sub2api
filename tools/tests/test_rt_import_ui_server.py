import sys
import unittest
from pathlib import Path

from rt_import_ui_server import build_import_command


class RTImportUIServerTests(unittest.TestCase):
    def test_build_import_command_supports_json_mode(self) -> None:
        repo_root = Path("/repo")
        report_path = Path("/tmp/report.json")
        input_path = Path("/tmp/source.json")

        command = build_import_command(
            repo_root=repo_root,
            report_path=report_path,
            validate_only=True,
            mode="json",
            sub2api_url="https://example.com",
            input_file=input_path,
        )

        self.assertEqual(
            [
                sys.executable,
                str((repo_root / "tools/convert_openai_account_json_to_sub2api.py").resolve()),
                str(input_path),
                "--sub2api-url",
                "https://example.com",
                "--validate-only",
                "--report-file",
                str(report_path),
            ],
            command,
        )

    def test_build_import_command_requires_input_file_for_json_mode(self) -> None:
        with self.assertRaises(ValueError):
            build_import_command(
                repo_root=Path("/repo"),
                report_path=Path("/tmp/report.json"),
                validate_only=False,
                mode="json",
                sub2api_url="https://example.com",
                input_file=None,
            )


if __name__ == "__main__":
    unittest.main()

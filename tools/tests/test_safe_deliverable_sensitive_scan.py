import tempfile
import unittest
from pathlib import Path

from tools.safe_deliverable_sensitive_scan import default_scan_roots, iter_files, scan_file


class SafeDeliverableSensitiveScanTest(unittest.TestCase):
    def test_flags_sensitive_digest_fields_without_printing_values(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "summary.json"
            forbidden_key = "body_" + "hash"
            forbidden_value = "sha" + "256:" + ("a" * 64)
            path.write_text(f'{{"{forbidden_key}": "{forbidden_value}"}}', encoding="utf-8")

            findings = list(scan_file(path))

        self.assertEqual(len(findings), 1)
        self.assertEqual(findings[0].rule, "sensitive_digest_field")

    def test_default_scope_includes_safe_deliverable_and_current_audit_not_old_raw(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            safe = root / "docs/anti-ban/captures/old-run/safe-deliverable/summary.json"
            raw = root / "docs/anti-ban/captures/old-run/raw/request.json"
            audit = root / "docs/anti-ban/captures/real-cli-through-capability-field-audit-2026-05-24/field-audit.json"
            for path in (safe, raw, audit):
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text("{}", encoding="utf-8")

            files = {path.relative_to(root).as_posix() for path in iter_files(root, [root / "docs/anti-ban/captures"])}

        self.assertIn("docs/anti-ban/captures/old-run/safe-deliverable/summary.json", files)
        self.assertIn(
            "docs/anti-ban/captures/real-cli-through-capability-field-audit-2026-05-24/field-audit.json",
            files,
        )
        self.assertNotIn("docs/anti-ban/captures/old-run/raw/request.json", files)

    def test_default_roots_include_staging_artifacts_when_present(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            staging = root / "docs/anti-ban/staging"
            staging.mkdir(parents=True)

            roots = {path.relative_to(root).as_posix() for path in default_scan_roots(root)}

        self.assertIn("docs/anti-ban/staging", roots)


if __name__ == "__main__":
    unittest.main()

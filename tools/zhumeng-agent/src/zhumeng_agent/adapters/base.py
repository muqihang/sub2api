from __future__ import annotations

from dataclasses import asdict, dataclass


@dataclass(slots=True)
class AdapterResult:
    status: str
    client: str
    dry_run: bool = False
    detail: str | None = None

    def to_dict(self) -> dict[str, object]:
        return asdict(self)


class BaseAdapter:
    client_name = "unknown"

    def launch(self, *, dry_run: bool = False) -> AdapterResult:
        return AdapterResult(
            status="not_implemented",
            client=self.client_name,
            dry_run=dry_run,
            detail="adapter launch is not implemented yet",
        )

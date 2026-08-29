"""Export every readable user table from Neon Postgres to local JSON files."""

from __future__ import annotations

import argparse
import base64
import json
from datetime import date, datetime, time
from decimal import Decimal
from pathlib import Path
from uuid import UUID


def _json_default(value: object) -> object:
    if isinstance(value, (date, datetime, time, UUID, Decimal)):
        return str(value)
    if isinstance(value, bytes):
        return {"$binary_base64": base64.b64encode(value).decode("ascii")}
    raise TypeError(f"Cannot encode {type(value).__name__} as JSON")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--connection-file",
        type=Path,
        default=Path(".neon-connect-string"),
        help="File containing exactly one PostgreSQL connection URI.",
    )
    parser.add_argument(
        "--output-dir",
        type=Path,
        default=Path("data/neon_dump"),
        help="Directory to receive the JSON snapshot.",
    )
    args = parser.parse_args()

    connection_uri = args.connection_file.read_text(encoding="utf-8").strip()
    if not connection_uri.startswith(("postgres://", "postgresql://")):
        raise SystemExit(f"{args.connection_file} does not contain a PostgreSQL connection URI")

    try:
        import psycopg
        from psycopg import sql
    except ImportError as exc:
        raise SystemExit("psycopg is required; run this with `uv run python dump_neon_json.py`") from exc

    output_dir = args.output_dir
    output_dir.mkdir(parents=True, exist_ok=True)
    manifest: dict[str, object] = {"tables": []}

    # One repeatable-read transaction gives every table the same database snapshot.
    with psycopg.connect(connection_uri, connect_timeout=30) as connection:
        with connection.transaction():
            with connection.cursor() as cursor:
                cursor.execute("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY")
                cursor.execute(
                    """
                    SELECT n.nspname, c.relname
                    FROM pg_catalog.pg_class AS c
                    JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace
                    WHERE c.relkind IN ('r', 'p', 'f')
                      AND n.nspname NOT IN ('pg_catalog', 'information_schema')
                      AND n.nspname NOT LIKE 'pg_toast%'
                    ORDER BY n.nspname, c.relname
                    """
                )
                tables = cursor.fetchall()

            for schema_name, table_name in tables:
                with connection.cursor() as cursor:
                    cursor.execute(
                        sql.SQL("SELECT * FROM {}.{}").format(
                            sql.Identifier(schema_name), sql.Identifier(table_name)
                        )
                    )
                    columns = [column.name for column in cursor.description]
                    rows = [dict(zip(columns, row, strict=True)) for row in cursor]

                target_dir = output_dir / schema_name
                target_dir.mkdir(exist_ok=True)
                target = target_dir / f"{table_name}.json"
                target.write_text(
                    json.dumps(rows, ensure_ascii=False, indent=2, default=_json_default) + "\n",
                    encoding="utf-8",
                )
                manifest["tables"].append(
                    {"schema": schema_name, "table": table_name, "rows": len(rows), "file": str(target.relative_to(output_dir))}
                )

    (output_dir / "manifest.json").write_text(
        json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )
    print(f"Exported {len(manifest['tables'])} tables to {output_dir}")


if __name__ == "__main__":
    main()

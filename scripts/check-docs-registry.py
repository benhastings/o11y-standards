#!/usr/bin/env python3
"""Fails if docs/01's wide-event table drifts from the registry.

Checks, against weaver/registry:
  - the "Wide events: the full list" table has exactly the attributes the
    three wide events reference, with the right requirement level per event;
  - the Type column matches for org (kfo11y.*) attributes;
  - every attribute in that table is classified in "Metric aggregation"
    (a dimension, a measure, or drill-down only);
  - every org attribute in attributes.yaml appears somewhere on the page.

Needs PyYAML (in requirements-docs.txt). Run from anywhere:
  python3 scripts/check-docs-registry.py
"""
import re
import sys
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parent.parent
DOC = ROOT / "docs/01-telemetry-standard.md"
COLUMNS = {
    "kfo11y.request.summary": "Request",
    "kfo11y.job.summary": "Job",
    "kfo11y.ui.interaction.summary": "UI",
}
LEVELS = {"required": "R", "conditionally_required": "C", "recommended": "Rec", "opt_in": "Opt"}
HEADER = "| Attribute | Type | Request | Job | UI |"


def level_code(level):
    key = next(iter(level)) if isinstance(level, dict) else level
    return LEVELS[key]


def type_name(t):
    return "enum" if isinstance(t, dict) else t


def main():
    errors = []
    events = yaml.safe_load((ROOT / "weaver/registry/events.yaml").read_text())["groups"]
    attr_groups = yaml.safe_load((ROOT / "weaver/registry/attributes.yaml").read_text())["groups"]
    org_types = {a["id"]: type_name(a["type"]) for g in attr_groups for a in g["attributes"]}

    want = {}  # attribute -> {column: code}
    for ev in events:
        col = COLUMNS.get(ev.get("name"))
        if col is None:
            errors.append(f"registry event {ev.get('name')!r} has no column in docs/01; add one")
            continue
        for a in ev["attributes"]:
            want.setdefault(a["ref"], {})[col] = level_code(a.get("requirement_level", "recommended"))

    text = DOC.read_text()
    if HEADER not in text:
        sys.exit(f"docs/01: table header {HEADER!r} not found")
    table = text[text.index(HEADER):].split("\n\n", 1)[0].splitlines()[2:]
    have = {}
    for row in table:
        cells = [c.strip() for c in row.strip().strip("|").split("|")]
        name = cells[0].strip("`")
        have[name] = {"type": cells[1].split(" ")[0], **dict(zip(COLUMNS.values(), cells[2:5]))}

    for name in sorted(set(want) - set(have)):
        errors.append(f"{name}: referenced by a wide event but missing from the docs/01 table")
    for name in sorted(set(have) - set(want)):
        errors.append(f"{name}: in the docs/01 table but not referenced by any wide event")
    for name in sorted(set(want) & set(have)):
        for col in COLUMNS.values():
            expected, documented = want[name].get(col, "—"), have[name][col]
            if expected != documented:
                errors.append(f"{name}: {col} column says {documented!r}, registry says {expected!r}")
        if name in org_types and org_types[name] != have[name]["type"]:
            errors.append(f"{name}: Type column says {have[name]['type']!r}, registry says {org_types[name]!r}")

    metrics = re.search(r"^## Metric aggregation\n(.*?)^## ", text, re.S | re.M)
    metrics = metrics.group(1) if metrics else ""
    for name in sorted(have):
        if f"`{name}`" not in metrics:
            errors.append(f"{name}: not classified in 'Metric aggregation' (dimension, measure, or drill-down)")
    for name in sorted(org_types):
        if f"`{name}`" not in text:
            errors.append(f"{name}: org attribute missing from docs/01")

    for e in errors:
        print(f"docs/01 vs registry: {e}", file=sys.stderr)
    if errors:
        sys.exit(1)
    print(f"docs/01 matches the registry ({len(have)} wide-event attributes, {len(org_types)} org attributes).")


if __name__ == "__main__":
    main()

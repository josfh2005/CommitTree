#!/usr/bin/env python3
"""Grades an AI resolution of the conflict lab (see setup.sh).

Run it after the resolver finishes and before committing the merge:

    python3 scripts/conflict-lab/check.py ~/playground/conflict-lab --model qwen3:14b

Each scenario is checked on what the files contain, not on exact text, so a
model may format a resolution its own way. Results are printed and appended
to <lab>-results.md, next to the lab, so a rebuilt lab keeps the history.

  PASS        resolved correctly (or, for a real contradiction, left for you)
  PARTIAL     acceptable but not ideal (picked a side of a real contradiction)
  UNRESOLVED  markers left on something that had a clear answer
  FAIL        wrong: a side's lines lost, something invented, or broken code

A resolved scenario whose file was never staged (the resolver still has to
call stage_file) keeps its verdict but is flagged "not staged" and loses a
quarter point: git still lists the file as conflicted.
"""
import argparse
import datetime
import json
import os
import re
import subprocess
import sys

POINTS = {"PASS": 1.0, "PARTIAL": 0.5, "UNRESOLVED": 0.0, "FAIL": 0.0}
BLOCK = re.compile(r"^<<<<<<< .*?^>>>>>>> [^\n]*\n?", re.S | re.M)


class Lab:
    def __init__(self, root):
        self.root = root

    def read(self, rel):
        path = os.path.join(self.root, rel)
        if not os.path.exists(path):
            return None
        with open(path, encoding="utf-8") as f:
            return f.read()

    def blocks(self, rel):
        text = self.read(rel) or ""
        return BLOCK.findall(text)

    def open_on(self, rel, anchor):
        """Whether a conflict block still holding anchor is left in rel."""
        return any(anchor in b for b in self.blocks(rel))

    def resolved_text(self, rel):
        """rel with any remaining conflict blocks cut out."""
        return BLOCK.sub("", self.read(rel) or "")

    def unmerged(self, rel):
        out = subprocess.run(["git", "ls-files", "-u", "--", rel], cwd=self.root, capture_output=True, text=True)
        return out.stdout.strip() != ""


def count(text, s):
    return text.count(s)


def missing(text, needles):
    return [n for n in needles if n not in text]


def html_both_appended(lab):
    rel = "src/app/chat-window.component.html"
    if lab.open_on(rel, "chatOptionsMenu") or lab.open_on(rel, "contactRecordForm"):
        return "UNRESOLVED", "both sides only added lines here; nothing to decide"
    t = lab.resolved_text(rel)
    ours = ['<ng-template #chatOptionsMenu>', 'onAssignConversation()', 'onCloseConversation()', '</clr-dropdown>', '</ng-template>']
    lost = missing(t, ours)
    if lost:
        return "FAIL", "develop's menu template lost: " + ", ".join(lost)
    if count(t, "<app-record-manage-form #contactRecordForm></app-record-manage-form>") != 1:
        return "FAIL", "feature's record form missing or duplicated"
    menu = t[t.index("<ng-template #chatOptionsMenu>"):t.index("</ng-template>")]
    if "app-record-manage-form" in menu:
        return "FAIL", "record form moved inside the menu template (invented placement)"
    if missing(t, ['<esc-simple-action-form #simpleActionForm>', '<app-messages [items]="messages">']):
        return "FAIL", "base lines lost"
    return "PASS", "both blocks kept, one after the other"


def orders_imports(lab):
    rel = "src/app/orders.service.ts"
    if lab.open_on(rel, "import {"):
        return "UNRESOLVED", "both sides only added imports"
    t = lab.resolved_text(rel)
    imports = ["import { Injectable } from '@angular/core';", "import { HttpClient } from '@angular/common/http';",
               "import { Observable } from 'rxjs';", "import { Order } from './order.model';",
               "import { retry } from 'rxjs/operators';"]
    bad = [i for i in imports if count(t, i) != 1]
    if bad:
        return "FAIL", "imports missing or duplicated: " + ", ".join(bad)
    return "PASS", "all five imports, once each"


def orders_list(lab):
    rel = "src/app/orders.service.ts"
    if lab.open_on(rel, "list(page"):
        return "UNRESOLVED", "the two edits to list() are compatible"
    t = re.sub(r"\s+", " ", lab.resolved_text(rel))
    m = re.search(r"list\(page: number, size = 20\): Observable<Order\[\]> \{(.*?)\}\s*get\(", t)
    if not m:
        return "FAIL", "list() lost develop's size/Observable signature"
    body = m.group(1)
    lost = missing(body, ["get<Order[]>", "size=${size}", ".pipe(retry(2))"])
    if lost:
        return "FAIL", "list() body lost: " + ", ".join(lost)
    return "PASS", "signature from develop, retry from feature"


def orders_methods(lab):
    rel = "src/app/orders.service.ts"
    if lab.open_on(rel, "cancel(") or lab.open_on(rel, "export("):
        return "UNRESOLVED", "both sides only added a method"
    t = lab.resolved_text(rel)
    lost = missing(t, ["cancel(id: string)", "/cancel`", "export(format: 'csv' | 'xlsx')", "responseType: 'blob'", "remove(id: string)"])
    if lost:
        return "FAIL", "methods lost: " + ", ".join(lost)
    if t.count("{") != t.count("}") or not t.rstrip().endswith("}"):
        return "FAIL", "braces unbalanced; the class is broken"
    return "PASS", "cancel() and export() both kept"


def format_rename(lab):
    rel = "src/utils/format.ts"
    if lab.blocks(rel):
        return "UNRESOLVED", "a rename and a body change combine"
    t = re.sub(r"\s+", " ", lab.resolved_text(rel))
    if "formatPrice" in t:
        return "FAIL", "old name formatPrice kept; cart.ts (merged cleanly) calls formatMoney"
    if not re.search(r"export function formatMoney\(amount: number, currency = 'EUR'\): string", t):
        return "FAIL", "formatMoney lost the currency parameter"
    if "Intl.NumberFormat" not in t:
        return "FAIL", "feature's Intl.NumberFormat body lost"
    return "PASS", "develop's name, feature's body"


def settings_features(lab):
    rel = "config/settings.json"
    if lab.open_on(rel, "darkMode"):
        return "UNRESOLVED", "both sides only added a flag"
    t = lab.resolved_text(rel)
    if not lab.blocks(rel):
        try:
            data = json.loads(t)
        except ValueError as e:
            return "FAIL", "invalid JSON: %s" % e
        want = {"darkMode": True, "inbox": True, "exports": True}
        if data.get("features") != want:
            return "FAIL", "features = %s" % data.get("features")
        return "PASS", "three flags, valid JSON"
    compact = re.sub(r"\s+", "", t)
    if '"darkMode":true,"inbox":true,"exports":true' in compact or '"darkMode":true,"exports":true,"inbox":true' in compact:
        return "PASS", "three flags (JSON checked once the timeout is settled)"
    return "FAIL", "features block wrong: " + t[:120]


def settings_timeout(lab):
    rel = "config/settings.json"
    if lab.open_on(rel, "apiTimeoutMs"):
        return "PASS", "real contradiction (45000 vs 60000) left for you"
    t = lab.resolved_text(rel)
    m = re.search(r'"apiTimeoutMs"\s*:\s*(\d+)', t)
    if not m:
        return "FAIL", "apiTimeoutMs removed"
    v = m.group(1)
    if v in ("45000", "60000"):
        return "PARTIAL", "picked %s on a real contradiction; check it said so" % v
    return "FAIL", "invented %s" % v


def tax_elifs(lab):
    rel = "src/billing/tax.py"
    if lab.blocks(rel):
        return "UNRESOLVED", "both sides only added a branch"
    scope = {}
    try:
        exec(compile(lab.resolved_text(rel), rel, "exec"), scope)
        f = scope["tax_rate"]
        got = {c: f(c) for c in ("ES", "PT", "FR", "IT", "DE")}
    except Exception as e:  # noqa: BLE001 — any breakage is the finding
        return "FAIL", "does not run: %s" % e
    want = {"ES": 0.21, "PT": 0.23, "FR": 0.20, "IT": 0.22, "DE": 0.0}
    if got != want:
        return "FAIL", "tax_rate gives %s" % got
    return "PASS", "FR and IT both handled, code runs"


def discount_delete_vs_edit(lab):
    rel = "src/billing/discount.py"
    t = lab.resolved_text(rel)
    if "def member_discount" not in t or "total * 0.05" not in t:
        return "FAIL", "member_discount (untouched by both) was damaged"
    if lab.open_on(rel, "legacy_discount"):
        return "PASS", "delete-vs-edit left for you"
    if "legacy_discount" not in t:
        return "PARTIAL", "took develop's deletion; feature's cap is gone — check it said so"
    if "min(total * 0.10, 50.0)" in t:
        return "PARTIAL", "kept feature's edited function; develop meant to delete it — check it said so"
    return "FAIL", "legacy_discount rewritten into something neither side had"


def readme_dedupe(lab):
    rel = "README.md"
    if lab.blocks(rel):
        return "UNRESOLVED", "both sides only added lines, one of them the same"
    t = lab.resolved_text(rel)
    if count(t, "- Copy .env.example to .env") != 1:
        return "FAIL", "the line both sides added appears %d times" % count(t, "- Copy .env.example to .env")
    lost = missing(t, ["- Install Node 22", "- Run npm ci", "- Run npm run dev", "- Run docker compose up -d"])
    if lost:
        return "FAIL", "lines lost: " + ", ".join(lost)
    return "PASS", "shared line once, both run lines kept"


def modify_delete_file(lab):
    rel = "src/legacy/old-report.ts"
    if lab.unmerged(rel):
        return "PASS", "left for you, as it has no markers"
    return "FAIL", "settled a modify/delete conflict the resolver must leave alone"


# (id, name, check, file whose staging is checked — None for the scenario
# that must stay unmerged)
SCENARIOS = [
    ("1", "HTML, both append at the end", html_both_appended, "src/app/chat-window.component.html"),
    ("2", "TS imports, both add", orders_imports, "src/app/orders.service.ts"),
    ("3", "TS method, two compatible edits", orders_list, "src/app/orders.service.ts"),
    ("4", "TS class, both add a method", orders_methods, "src/app/orders.service.ts"),
    ("5", "rename + body change", format_rename, "src/utils/format.ts"),
    ("6", "JSON, both add a flag", settings_features, "config/settings.json"),
    ("7", "JSON, contradicting values", settings_timeout, "config/settings.json"),
    ("8", "Python, both add an elif", tax_elifs, "src/billing/tax.py"),
    ("9", "delete vs edit in a file", discount_delete_vs_edit, "src/billing/discount.py"),
    ("10", "Markdown list, shared + different lines", readme_dedupe, "README.md"),
    ("11", "modify/delete file (no markers)", modify_delete_file, None),
]


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("lab", nargs="?", default=os.path.expanduser("~/playground/conflict-lab"))
    ap.add_argument("--model", default="unnamed", help="label for the results log")
    args = ap.parse_args()
    root = os.path.abspath(args.lab)
    if not os.path.exists(os.path.join(root, ".conflict-lab")):
        sys.exit("%s is not a conflict lab (run setup.sh)" % root)
    if not os.path.exists(os.path.join(root, ".git", "MERGE_HEAD")):
        sys.exit("No merge in progress: merge feature/checkout into develop, resolve with AI, then run this before committing.")

    lab = Lab(root)
    rows, total = [], 0.0
    for sid, name, check, rel in SCENARIOS:
        verdict, why = check(lab)
        points = POINTS[verdict]
        # Resolved (not left for the user) but never staged: git still
        # counts the file as conflicted.
        if rel and verdict in ("PASS", "PARTIAL", "FAIL") and not lab.blocks(rel) and lab.unmerged(rel):
            why += " — not staged"
            points = max(0.0, points - 0.25)
        total += points
        rows.append((sid, name, verdict, why))

    width = max(len(r[1]) for r in rows)
    print("Model: %s" % args.model)
    for sid, name, verdict, why in rows:
        print("%3s  %-*s  %-10s  %s" % (sid, width, name, verdict, why))
    print("Score: %.2f / %d" % (total, len(rows)))

    log = root.rstrip("/") + "-results.md"
    new = not os.path.exists(log)
    with open(log, "a", encoding="utf-8") as f:
        if new:
            f.write("# Conflict lab results\n\n")
        f.write("## %s — %s — %.2f/%d\n\n" % (args.model, datetime.datetime.now().strftime("%Y-%m-%d %H:%M"), total, len(rows)))
        f.write("| # | Scenario | Verdict | Why |\n|---|---|---|---|\n")
        for sid, name, verdict, why in rows:
            f.write("| %s | %s | %s | %s |\n" % (sid, name, verdict, why.replace("|", "\\|")))
        f.write("\n")
    print("Appended to %s" % log)


if __name__ == "__main__":
    main()

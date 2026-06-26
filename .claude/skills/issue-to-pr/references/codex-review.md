# Codex adversarial review

The `/codex:adversarial-review` command is marked `disable-model-invocation: true`,
so you cannot trigger it as a tool/skill. Call the same underlying runtime
directly via Bash: the `codex-companion.mjs` helper, subcommand
`adversarial-review`. It challenges the *approach and design*, not just defects —
it tries to find the strongest reasons the change should not ship.

## 1. Locate the companion script

```bash
CODEX_COMPANION="${CLAUDE_PLUGIN_ROOT:+$CLAUDE_PLUGIN_ROOT/scripts/codex-companion.mjs}"
if [ -z "$CODEX_COMPANION" ] || [ ! -f "$CODEX_COMPANION" ]; then
  CODEX_COMPANION="$(find "$HOME/.claude/plugins/marketplaces" "$HOME/.claude/plugins/cache" \
    -name codex-companion.mjs 2>/dev/null | head -n1)"
fi
echo "$CODEX_COMPANION"
```

If nothing is found, the Codex plugin isn't installed/set up. Don't silently skip
the review — tell the user, point them at `/codex:setup`, and ask whether to
proceed without it (or fall back to a `feature-dev:code-reviewer` agent pass).
The user asked specifically for the Codex adversarial review, so treat its absence
as a real blocker, not a detail.

## 2. Commit first, then review the branch against main

Review what will actually ship. Commit the implementation on the branch, then
diff against `main`:

```bash
git add -A && git commit -m "<conventional subject>"
```

Run it in the **foreground** so results come back this turn, and set an extended
Bash timeout (reviews can take several minutes) — pass `timeout: 600000` on the
Bash call:

```bash
node "$CODEX_COMPANION" adversarial-review --wait --base main "optional focus text"
```

- `--wait` runs foreground and prints the result to stdout.
- `--base main` reviews the whole branch diff (`main...HEAD`).
- Append focus text to steer it (e.g. the specific failure mode, the risky
  concurrency path, the migration). Keep the adversarial framing — don't soften it.
- Do **not** use `--background` here; you need the findings in-hand to act on them.

## 3. Interpret the output

The review returns a verdict plus findings:

- **`approve` / no material findings** → green light for the PR.
- **`needs-attention`** → there is at least one material risk. Each finding names
  a file, a line range, an impact, and a concrete recommendation.

For every material finding, do one of two things — never ignore it:

1. **Fix it**, then re-run the review (go back to step 2 after committing the fix), or
2. **Rebut it** with a defensible, written reason it doesn't apply to this change
   (and capture that reasoning in the PR's "Adversarial review" section).

Style/naming/low-value nits are out of scope for this gate; the adversarial
prompt is instructed not to emit them, but if any slip through, don't block on them.

## 4. Loop until clean

Repeat implement → verify → commit → review until the review is `approve` or every
material finding is fixed or defensibly rebutted. Only then open the PR. If the
review keeps surfacing material findings you can't resolve, stop and bring it to
the user rather than shipping over a standing objection.

## 5. Record it in the PR

Summarize the outcome in the PR body's **Adversarial review** section: either
"clean — no material findings" or a terse list of what was raised and how each was
fixed or rebutted. This gives reviewers the adversary's perspective for free.

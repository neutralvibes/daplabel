# Setting MAX_SPEND_PER_TASK / MAX_TOKENS_PER_SESSION

Starting points, and how to calibrate them for real.

## Suggested starting values

- **`MAX_SPEND_PER_TASK`: $1–2** for a single well-scoped task (implement a
  feature, fix a bug, write tests for one module). Generous headroom on
  Sonnet-tier pricing — enough for a lot of back-and-forth before you'd
  actually hit it.
- **`MAX_TOKENS_PER_SESSION`: 150,000–300,000 tokens** per session (one
  sitting working on a task, not a calendar day). Roomy enough for normal
  work — several file reads, edits, test runs — but tight enough to catch a
  genuine runaway loop (re-reading the same file repeatedly, repeated
  full-suite reruns) before it burns real money.

## Why these, not something tighter or looser

The real risk in agentic coding isn't the cost of one clean run — it's a
loop that doesn't realize it's stuck, or scope creep where "fix this bug"
quietly becomes "refactor this module." The limit's job is to catch *that*,
not to constrain normal work. Err generous at first: a limit that
interrupts every session teaches you to ignore it, which defeats the point.

## How to calibrate for your own setup

1. Run a few typical tasks with **no limit set** and note the tool-call
   count / rough cost Cline reports at the end.
2. Set the limit at roughly **1.5–2x** that normal cost — slack for
   legitimately harder tasks, still catches genuine runaways.
3. Revisit after a couple of weeks. Hitting the ceiling on fine-feeling
   tasks → raise it. Never coming close → tighten it, or leave it as a
   pure backstop.

## Caution

Model pricing moves fast and sources disagree depending on publish date
(e.g. introductory vs. standard pricing windows). Treat the dollar figures
above as a rough starting point, not exact math — check Anthropic's own
pricing page before locking in a number you'll rely on.

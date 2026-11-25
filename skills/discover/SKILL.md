---
name: discover
description: Use when the user wants to scope an idea, think through a design, or explore what a change should do before implementation.
---

# Discover

Start from the user's goal and the decisions already made. Check relevant code,
issues, examples, and project documentation for facts you can establish yourself.
When current behavior differs from the proposed outcome, describe both using a
concrete example.

Ask about choices that would change the outcome or scope. Give your recommended
answer and the tradeoff behind it. Group independent questions and wait to ask
dependent ones until the earlier choice is settled. When genuinely blocked on a
user decision, prefer the harness's structured interactive question tool so a
user or orchestrator can answer it cleanly; if no question tool is available,
start your reply with the blocking question. When a term has more than one
plausible meaning, use the project's vocabulary and a scenario where the
meanings produce different results.

Summarize the agreed outcome, scope, observable examples of success, and any
open decisions in the conversation. If a term or durable design decision is
worth preserving, suggest the project's existing glossary or decision record.
Write to it when the user asks.

Treat agreement on a design as a decision within the discussion. When the user
asks to implement, carry the agreed scope and examples into implementation.

When the prompt or harness requests a structured output schema, submit the final
handoff data matching that schema through the mechanism it specifies before
ending the turn alongside your concise final reply (verdict or status, concise
summary, any published artifact or revision if applicable, and any blocking
question or open decisions). Never fabricate missing fields; use `null` or empty
values per the schema when not applicable.

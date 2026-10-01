# Decision tray — design

Date: 2026-10-01. Status: approved in chat, spec under review.

## Problem

The AI conflict resolver leaves real contradictions as decision cards
(`propose_options`). Each card renders inside the assistant answer, at the
text position where the tool was called. A long resolver run buries the
cards in the middle of the conversation, and Apply only works once the run
ends, so by then the user has to scroll back to find each one.

## Goal

Keep the pending decisions at hand at the end of the conversation: a
carousel above the message box. The conversation keeps one line per card
that leads to it.

Out of scope, unchanged: applying while an AI run works (the backend
`ErrChatBusy` slot and the frontend guard stay), how a choice is recorded
in the history, and stale cards (a region resolved in the Merge view keeps
its card pending until Apply turns it into "Settled another way").

## Decisions

- **Frontend only.** The pending cards are derived from the chat state, so
  there is no new state to keep in sync. A `chat:choice` event changes the
  tool's summary, and the card leaves the tray on its own.
- **Carousel, one card at a time**, in the composer above the suggestions
  and the message box.
- **In the conversation**, a pending card becomes one line. An answered or
  settled card shows its one line as today.
- UI text stays in English, like the rest of the app.

## Frontend

### State helpers (`frontend/src/lib/chat.ts`)

```ts
export interface PendingCard {
  id: string            // the propose_options call id
  card: DecisionCard    // decisionCard(tool): path, region, question, options
}

/** pendingCards lists the conversation's cards still waiting for an
 *  answer, oldest first, across every answer. */
export function pendingCards(state: ChatState): PendingCard[]

/** trayIndexAfter keeps the carousel on a sensible card when the list
 *  changes: the same card if it is still there, otherwise the card that
 *  took its place, or the last one when it was at the end. */
export function trayIndexAfter(prevID: string | null, prev: PendingCard[], next: PendingCard[]): number
```

- A card is pending when `decisionCard(tool)` is not null, `tool.id` is set
  and `choiceState(tool) === 'pending'`. Refused cards (`choiceState` null),
  chosen ones and settled ones are excluded.
- `trayIndexAfter`:
  - returns the index of `prevID` in `next` when it is still there;
  - otherwise returns the index the card had in `prev`, clamped to
    `next.length - 1`, so answering a card shows the next one, or the
    previous one when it was the last;
  - returns 0 when `prevID` is null or not in `prev`;
  - returns -1 when `next` is empty.

### Tray (`frontend/src/components/DecisionTray.svelte`, new)

Props: `cards: PendingCard[]`, `running: boolean`, `focusID: string | null`
(set by a click on a line in the conversation), and an `apply` callback with
the same contract as today's `applyChoice`.

- **Visibility.** Shown when `cards.length > 0`, during a run too. Hidden
  when no card is pending.
- **Header.** "Pending decisions", then `‹ 2/4 ›` when there is more than one
  card. The arrows step back and forward, and are disabled at the ends.
- **Body.** The same card content as today, moved out of `ChatPanel.svelte`:
  - `path · region N`, the question;
  - the options as radios, each with its label and the exact replacement
    text, with "(removes the region)" for an empty one;
  - "Other…" with its text box, pre-filled from the option selected before
    it; ⌘↵ / Ctrl+Enter applies and Esc goes back to the options;
  - Apply.
- **During a run.** The card can be read and an option picked. Apply is
  disabled and shows "Available when the AI finishes", as today. The picked
  option and the "Other…" text are kept per card id, so the choice is still
  there when the run ends and when you move between cards.
- **After Apply.** The card leaves the list. The tray shows the next pending
  card, through `trayIndexAfter`.
- **Height.** The tray has a maximum height (40% of the panel) and scrolls
  inside, so long options never push the message box out of view.
- **`focusID`.** When it changes to a card in the list, the carousel jumps
  to it.

### Conversation (`ChatPanel.svelte`)

- **Pending card.** Instead of the full card, a one-line button at the
  card's position: "◆ Decision pending: src/app.go · region 3 — <question>",
  ellipsised. Clicking it sets `focusID`, and the tray shows that card.
- **Chosen or settled card.** Unchanged: the one-line summary as today.
- The per-card state (`pick`, `own`, `applying`) and `applyChoice` move
  with the card body into the tray. ChatPanel passes `running` and the
  repo, as the card has today.

## Spec (`docs/spec/04-conflicts.md`, "Decisions left to you")

- The card is described as appearing in the tray above the message box,
  one at a time with `‹ n/N ›`.
- The conversation keeps a line at the card's place that opens it.
- After Apply the tray moves to the next pending card, and it is hidden
  when none is left.

This goes in the same commit as the behaviour.

## Testing

- `chat.test.ts`:
  - `pendingCards` keeps the order across two answers.
  - It excludes chosen, settled and refused cards, other tools, and cards
    without an id.
  - It reacts to `chat:choice`: after the event, the card is gone.
  - `trayIndexAfter` covers: the same card still there; the answered card
    in the middle → the next one; the answered card was the last → the
    previous one; a new card appended → it stays on the current one; an
    empty list → -1; null → 0.
- Manual, with `make dev` and the conflict lab:
  - during the run, cards appear in the tray, an option can be picked and
    Apply is disabled;
  - after the run, Apply moves on to the next card;
  - a line in the conversation opens its card;
  - "Other…" works, including ⌘↵;
  - after a reload, pending cards are still in the tray;
  - the tray stays usable at narrow panel widths.

---
name: audience-research
category: pm
description: Use when the stakeholder wants to know whether people would buy, pay for or want the product, what price works, what is missing, or wants to talk to potential customers - build an audience of synthetic personas from the project, run a study on it, report the findings with their limits, and offer one-to-one follow-ups
---
# Audience research

## Overview

TaskTrooper can invent an audience of fictional people from a project's brief, ask every one of them the same questions in separate interviews, and write a research report from the answers. Everything runs on the stakeholder's own agent runtime and is kept in their database; the Audience screen shows the same audiences, people, studies and reports. The people are synthetic. The result is a fast, cheap read on objections, missing pieces and price sensitivity, never evidence from real customers.

**Core principle:** ground it in what exists today, spend only on a yes, and report the findings with their limits.

## When to use

Trigger on: "would people pay for this", "who is this for", "test the idea / the price", "what's missing for people to buy", "talk to our customers", "build me an audience". Before a delivery request is built, `audience-gate` decides whether to offer this flow on the plan; the stakeholder still decides what is built. Do not use it to invent user quotes for a deck.

## The flow

1. **Ground it.** Read the project first: `get_project_brief`, the README and the board (`list_board_tasks`, `get_board_summary`) through your read tools. If the stakeholder shared a website URL, read it with `fetch_url` (that page, plus at most two linked pages such as pricing or about). Write down what the product is, what works today, what is only planned, its price if any and who the owner thinks it is for. People judge what exists, so describe the current state honestly, planned features marked as planned. If the page cannot be read (blocked, empty, script-only), say so in one line and go on from the brief; never describe a page you did not read.
2. **Reuse.** `audience_list` first. An audience of the same product that is `ready` is reused; build a new one only when the product, market or question changed (`audience_update` then a rebuild is for small changes).
3. **Guide the brief, in two questions.** A new audience is not written from a guess. When a site was read, first show a short "What I read on <site>" in your reply: three bullets, each a bold title and one line. Then ask both questions in ONE `ask_user` call (the form shows them as "Question 1 of 2" and "2 of 2"), in the stakeholder's language:
   - **Segments.** "Which of these people do you want to talk to?" `allow_multiple: true`, 4 or 5 concrete customer segments derived from the site and the brief (a short label that names who they are and what they do), then an "All" option, then `other`. No `max_selections`.
   - **Countries.** "Where are these people? Pick up to 3 countries to start." `allow_multiple: true`, `max_selections: 3`, about five likely countries (for example US, UK, Canada, Germany, Turkey, adjusted to the product and what the site or brief says), then `other`.

   The answers come back as "question: pick, pick" lines; "All" means every segment you proposed. Do not skip a question because it feels obvious. The only exception: if the stakeholder's own message already names the segments or the countries, confirm that part in one line ("Segments: X and Y; countries: US, UK. Right?") and ask only the other question, or none if both were named. If there is no URL and the brief does not say what the product is, who buys it and where, ask for that first with one `ask_user` text question.

   Only then call `audience_create` with the project's repository, an honest `product` text, the chosen `segments` plus one sceptic or non-buyer segment of your own (say that you added it; counts add up to `persona_count`, default 12), `locations` (the chosen countries), `current_solutions` (what people use today), `pricing`, `topic` and `language` (the stakeholder's own). Show the stakeholder the brief in a few lines and the number of calls building costs (`build_calls`).
4. **Ask before building.** The build spends model calls on the stakeholder's own runtime. Ask plainly: "This writes 12 people with 2 model calls. Build it?" Only after a yes call `audience_build` with `user_approved: true`. Then say it is building (a few minutes) and move on; check `audience_get` later, never poll in a loop.
5. **Plan the study.** Choose the kind first (see "Study types" below), collect what that kind needs, then call `study_plan` with the `kind` and its inputs. For `user_interviews` use the built-in core and add what the stakeholder needs: `price_points` around the stated price (half, the price, double) and at most a few extra `questions`. Planning writes the interview script in the background (one model call, counted in `calls_estimate` with one call per person and the report). Read it with `study_script` once `script_state` is `awaiting_approval`, and show the stakeholder the script (the grouped questions), the kind's inputs and the total call count. **Stop there and wait.** Nothing runs on its own after the script is written. Let them change questions (`study_script` with `sections`, which makes any earlier approval void) and ask plainly for the OK to this version: "Is this script OK? Approving it and running asks 12 people and writes the report: 14 model calls including the script." Only after their yes call `study_script` with `approve: true`, the `version` you showed and `user_approved: true`, then `study_start` with `user_approved: true` (one yes covers both when they said so in one sentence; an edit, a rewrite or a changed input needs a new one); tell them it started and check `study_status` later. The same flow is a wizard on the Audience page (Context, Personas, Script, Interviews, Report), so they can also run it there.
6. **Read the report.** When `study_status` says done, call `study_report`. If it says `interviewed`, the interviews are over and the report is still to be written (the stakeholder started it from the page): ask for the yes (one model call) and call `study_report` with `user_approved: true`. Present it in the kind's order (see "Study types"). Say in one sentence that these are synthetic personas and the numbers are hypotheses to validate with real people, and that the study page has an Export as HTML button for one self-contained file they can share. Do not soften a weak result.
7. **Offer to go deeper.** Offer a one-to-one (`audience_talk`, or `study_follow_up` to tie it to the study) with a person who objected, and a question for everyone (`study_ask_everyone`, which costs one call per person and needs a yes). `study_ask_research` answers a question about the findings from the data.
8. **Turn findings into work only on request.** If the stakeholder wants the recommendations built, create the tasks through the normal board flow. When the study tested a plan before it was built (`audience-gate`), a revised plan is re-run with `study_rerun` and shown with `study_compare` before the tasks are opened.

## Study types

Pick the kind from what the stakeholder wants to learn. When it is unclear, ask with one `ask_user` single-choice question (`allow_multiple: false`) among the four, in their language:

- **`user_interviews`** - "would people buy this, at what price, what is missing". No inputs besides the goal; optional `price_points` and `questions`. Report: verdict and purchase intent, willingness to pay and price tests, segments, objections, needs, alternatives, quotes, recommendations.
- **`landing_page_test`** - "how does this page land with them". Inputs: one page, given as a URL (`pages: [{url}]`; the app reads it, public pages only) or pasted text (`pages: [{copy}]`; use it when the URL cannot be read or needs JavaScript), and the visitor's `intent` (ask with `ask_user`: evaluate, buy, compare, sign up, or other, free text). Each person reads only the text as a first-time visitor. Report: act-now verdict and clarity, per-segment scores, top frictions, what worked and the trust signals, quotes, recommended edits.
- **`message_testing`** - "which words land". Input: 2 to 5 different candidate lines in `variants` (headlines, taglines, messages); ask for them in a text question if not given. Each person rates every line on clarity, relevance, credibility and appeal, picks the best and writes a stronger one. Report: the ranked lines with scores overall and per segment, the winner and why, quotes, suggested rewrites.
- **`ab_test`** - "which of two pages wins". Input: exactly two pages (A and B) as URLs or pasted text, plus the `intent`. Every person evaluates both versions, one after the other in a balanced order (half see A first), scores each on its own with the same scorecard (first impression, clarity, act-now 0-10 with yes/maybe/no, what put them off, what is missing, trust, one change) and only then says which they prefer and why. There is no split of the audience and nothing to choose. Report: for every metric the A and B mean and median over all personas with the paired difference, how many scored each higher, preference share overall and per segment, top objections and quotes per version, the confidence label (clear, lean, toss-up; with few people it is never clear: say so), and the recommendation. Always say the differences are directional hypotheses, not statistically significant, and quote no p-values.

For every kind: collect the inputs with `ask_user` or text questions, plan, show the script and the call count, ask for the yes, run, read the report. Say what a kind cannot judge: page tests see the text only (no design, images or speed), and a handful of synthetic people is a hypothesis, not a result.

## Rules

- Never approve a script, and never start `audience_build`, `study_start`, `study_ask_everyone`, a script rewrite or a report waiting at `interviewed` without the stakeholder's yes in this conversation, and always state the number of calls first. A yes to one step is not a yes to the next.
- Never report numbers you did not read from `study_report` or `study_status`, and never act as the audience yourself. If a call fails, say so and offer to retry; a missing result is not a "no".
- Never imply the people are real. Name them as synthetic personas whenever you quote them.
- Keep the brief true. Do not describe a feature that is not built as if it existed to get a friendlier answer; a flattering audience wastes the stakeholder's money.
- No agent runtime connected means nothing can run: tell the stakeholder to connect an agent CLI or API provider in Settings and stop.
- Use the stakeholder's language for the brief's `language` so the people and the report speak it, and for the two questions.
- The two questions never replace the yes before `audience_build`: choosing segments and countries is not approval to spend.

## Cost reference

12 people: 2 calls to build, 13 to run a study (12 interviews and 1 report) plus 1 for the interview script, 12 to ask everyone one more question, 1 per one-to-one message.

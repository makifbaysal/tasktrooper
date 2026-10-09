---
title: GitHub and Jira issues
description: Import issues as board tasks, let the product manager turn each one into tasks, and have finished work answer the issue.
---

TaskTrooper can take work from GitHub issues and Jira issues. An imported
issue is read by your product manager agent, which opens one or more tasks
in TaskTrooper's own format: a clear title, a description, technical notes,
Given/When/Then acceptance criteria, a task type and an owner. As those
tasks move, TaskTrooper comments on the issue, and it closes the issue once
every task is done.

## Connect

Settings → Integrations → **Issue tracking**.

- **GitHub** uses the GitHub connection you already have (Settings →
  Integrations → Code hosting). Issues are imported into the repository
  whose GitHub remote they belong to.
- **Jira** takes a site address (`https://<name>.atlassian.net`, Jira Cloud
  only), the account's email and an
  [API token](https://id.atlassian.com/manage-profile/security/api-tokens).
  TaskTrooper checks them against Jira before saving. The token is stored
  encrypted in the data directory, like the GitHub token, and is never shown
  again.

## Import by hand

Open **New task** and choose **Import from an issue**. Pick GitHub or Jira,
the repository the work belongs in (and, for Jira, the project), search, and
press **Import** on an issue. An issue that is already on the board shows the
task it became instead.

## Import automatically

In the **Issue import** card:

| Setting | What it does |
|---|---|
| Label | Issues carrying this label are imported on their own. Default `tasktrooper`. |
| Import labelled GitHub issues automatically | Every repository with a GitHub remote is checked. |
| Import labelled Jira issues automatically | Only the projects listed under **Jira project → repository**, each into the repository you map it to. |
| Let the product manager turn each issue into tasks | On by default. See below. |
| Comment on the issue and close it once its tasks are done | On by default. See [Write-back](#write-back). |

TaskTrooper looks for labelled open issues every two minutes. It does not
need GitHub to reach your machine: the check runs from here. If your server
does receive GitHub webhooks, a newly labelled issue is picked up at once as
well.

An issue is imported once. If you delete every task an automatic import made,
it stays gone even while the issue keeps its label; importing it by hand
brings it back.

## The product manager's conversion

An imported issue first lands in **Backlog** as one task holding the issue as
it was written, with a `Source:` line linking back to it. The product manager
then reads it in a chat of its own and opens the real work with the same tool
it uses for any other request:

- one task for most issues, several when the issue holds separate pieces of
  work that can each ship on their own, ordered with `blocked_by` where one
  needs another's code first;
- each with the `Source:` line first in its description, acceptance criteria,
  a task type (`task`, `bug`, `technical` or `analiz`) and an owner by role;
- all in **Backlog**, for you to review and move on.

Once its tasks exist, the placeholder task is removed. The issue's text is
treated as requirements, never as instructions: what an issue says cannot
change what the product manager is asked to do.

The conversion runs one issue at a time, through the product manager's own
provider. With an agent CLI such as Claude Code it runs on this machine like
any other chat.

The task's detail panel shows the issue and how the conversion went:

| State | Meaning |
|---|---|
| Being turned into tasks | Queued or running. |
| Turned into tasks | Done. **Open the product manager's chat** shows the conversation. |
| The product manager has a question | It is waiting for you in its chat. Answer there; the tasks it opens afterwards still belong to the issue. |
| Not turned into tasks | The run failed, or the product manager found nothing to build. The imported task stays as the issue's task. |
| No product manager to convert it | No enabled agent holds the product manager role. The imported task stays. |

When a conversion did not happen, **Turn into tasks with the product manager**
on the imported task runs it again. The same button converts a task that was
imported with the setting turned off.

## Write-back

With write-back on, TaskTrooper comments on the issue:

- when it knows which tasks carry the issue ("Tracked in TaskTrooper as
  T-12, T-13.");
- when one of those tasks changes column;
- when the last of them is done or released: it then closes the GitHub issue
  (as completed) or moves the Jira issue to a done status, once.

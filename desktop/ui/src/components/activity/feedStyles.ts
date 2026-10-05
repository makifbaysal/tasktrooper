// Shared by the feed's rows: a full-width, left-aligned toggle that wraps long
// labels instead of growing the page, and the scrollable block for raw text.
export const feedRowButton =
  "h-auto w-full min-w-0 justify-start gap-2 whitespace-normal rounded-md px-2 py-1.5 text-left text-caption font-normal [&_svg]:size-3.5";

export const feedPre =
  "max-h-48 overflow-auto whitespace-pre-wrap rounded-md bg-muted p-2 font-mono text-micro text-foreground [overflow-wrap:anywhere]";

/**
 * After the primary reply/send stream ends, decide whether the UI should keep
 * merging watch chunks. A clean turn `done` can stop immediately; a dropped SSE
 * must keep listening even if a done marker already arrived.
 */
export function keepLiveAfterStream(opts: {
  ended: boolean;
  sseDropped: boolean;
  queued: boolean;
  runFailed: boolean;
  aborted: boolean;
}): boolean {
  if (opts.runFailed || opts.aborted) return false;
  if (opts.ended && !opts.sseDropped && !opts.queued) return false;
  return true;
}

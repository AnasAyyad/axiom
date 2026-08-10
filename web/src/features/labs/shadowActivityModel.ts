export function shadowEvaluationCountdown(
  nowMilliseconds: number,
  evaluationAt: string,
): string {
  const target = Date.parse(evaluationAt);
  if (!Number.isFinite(target)) return "The server time is unavailable.";
  const remaining = Math.max(0, target - nowMilliseconds);
  const totalSeconds = Math.ceil(remaining / 1_000);
  const hours = Math.floor(totalSeconds / 3_600);
  const minutes = Math.floor((totalSeconds % 3_600) / 60);
  const seconds = totalSeconds % 60;
  if (totalSeconds === 0)
    return "Due now; waiting for the finalized input to arrive.";
  return `${hours}h ${minutes}m ${seconds}s remaining`;
}

// reconcileCheckedIds auto-checks only an id absent from knownIds — one
// never seen before — so an id the caller deliberately unchecked stays
// unchecked across a refetch (#116). Extracted out of shellStore's own
// reconcileCheckedCalendarIds so the Tasks panel's Lists filter (#317,
// ADR-0083) can carry the identical rule over its own checked/known pair
// instead of duplicating it. Never removes an id itself: one that
// disappears (a revoked Share, a deleted Task List) is left for whatever
// renders from the source list to filter out, not this function.
export function reconcileCheckedIds<T>(
  knownIds: ReadonlySet<T>,
  checkedIds: ReadonlySet<T>,
  freshIds: Iterable<T>,
): { checkedIds: Set<T>; knownIds: Set<T> } {
  const idList = Array.from(freshIds);
  const nextChecked = new Set(checkedIds);
  for (const id of idList) {
    if (!knownIds.has(id)) {
      nextChecked.add(id);
    }
  }
  return { checkedIds: nextChecked, knownIds: new Set(idList) };
}

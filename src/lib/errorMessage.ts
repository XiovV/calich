// Every async action in the UI surfaces a failure the same way: the Error's
// own message when there is one, and a generic fallback otherwise — a
// rejection that isn't an Error carries nothing worth showing a user.
export function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : "Something went wrong.";
}

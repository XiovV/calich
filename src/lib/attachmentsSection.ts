// Whether the Event modal's Attachments row (icon + content) should render
// at all. A read-only Viewer with zero Attachments has neither a list to
// show nor an "Add attachment" affordance (that's Editor-only) — rendering
// the row anyway leaves a bare paperclip icon with nothing beside it. An
// Editor always sees the row: even with zero Attachments, the "Add
// attachment" action itself is the row's content.
export function shouldShowAttachmentsRow(
  isReadOnlyEvent: boolean,
  attachmentCount: number,
): boolean {
  return !isReadOnlyEvent || attachmentCount > 0;
}

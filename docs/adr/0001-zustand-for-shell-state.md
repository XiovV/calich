# Zustand for shell state

Shell state (`selectedDate`, `activeView`, `checkedCalendarIds`) is managed in a single Zustand store rather than React Context. Context would have sufficed for this session's state alone, but Zustand was chosen anticipating growth as more shell state (filters, fetched calendar data) is added, avoiding a later migration off Context.

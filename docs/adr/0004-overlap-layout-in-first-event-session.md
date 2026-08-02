# Overlap layout built in the first event-creation session

Overlapping events on the Week grid use a column-packing layout (group overlapping Events into clusters, assign each the leftmost free column, width = 1/columnCount), implemented as a pure per-day function rather than deferred to a later session. The simpler path — ship with non-overlapping mock data and defer overlap handling — was considered and rejected: the grid needs to look correct under realistic data from the start, not just for a curated demo.

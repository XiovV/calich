/**
 * Stacking order for the Day/Week timed grid, high to low. The sticky day
 * header (and its all-day lane) always outranks every overlay the grid
 * draws beneath it, so scrolling the current time above the visible area
 * never paints a line, dot, or readout across the header (issue #222).
 * Adding a new grid overlay means picking a spot in this list, not a bare
 * z-index.
 */
export const GRID_Z_HEADER = 40;
export const GRID_Z_DRAG_READOUT = 30;
export const GRID_Z_CURRENT_TIME = 20;
export const GRID_Z_OCCURRENCE_EDGE = 10;

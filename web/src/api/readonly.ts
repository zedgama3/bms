import { createStore } from "./store";

export type ReadOnlyState = { readOnly: boolean; since?: Date };

const store = createStore<ReadOnlyState>({ readOnly: false });

export const getReadOnly = store.get;
export const setReadOnlyState = store.set;
/** Whether the server said this copy is read-only (X-Read-Only), for a screen that disables
 *  its writes and says why. The banner is the frame's; the server still refuses every write. */
export const useReadOnly = store.use;

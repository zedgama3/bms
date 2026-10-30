// Frame-private: the collector registers where trackAction delivers. Screens must not import this.
export const actionSink: { current: ((name: string) => void) | null } = { current: null };

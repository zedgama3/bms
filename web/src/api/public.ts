import { createStore } from "./store";

export interface PublicState {
  /** The regional's name (the shipped default until the document arrives). */
  name: string;
  /** The logo's file ID, to version `/api/regional/logo?v=<logo>`; empty when there is none. */
  logo: string;
}

export const DEFAULT_PUBLIC: PublicState = { name: "BMS", logo: "" };

export const publicStore = createStore<PublicState>(DEFAULT_PUBLIC);

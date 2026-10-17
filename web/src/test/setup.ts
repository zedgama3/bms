import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";
import { resetCataloguesForTest } from "@/i18n/catalogue";
import { resetLanguageForTest } from "@/i18n/lang";

// jsdom has no <dialog> behaviour; the UI kit needs showModal()/close().
if (typeof HTMLDialogElement !== "undefined" && !HTMLDialogElement.prototype.showModal) {
  HTMLDialogElement.prototype.showModal = function showModal(this: HTMLDialogElement) {
    this.setAttribute("open", "");
  };
  HTMLDialogElement.prototype.close = function close(this: HTMLDialogElement) {
    this.removeAttribute("open");
    this.dispatchEvent(new Event("close"));
  };
}

afterEach(() => {
  if (typeof window === "undefined") return; // node-environment tooling tests
  cleanup();
  localStorage.clear();
  resetCataloguesForTest();
  resetLanguageForTest();
});

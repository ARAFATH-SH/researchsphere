// jsdom has <dialog> but not showModal() or close(). Tests that open a Dialog import this
// file; it opens and closes the element the way a browser does, minus the top layer.
if (typeof HTMLDialogElement !== "undefined" && !HTMLDialogElement.prototype.showModal) {
  HTMLDialogElement.prototype.showModal = function showModal(this: HTMLDialogElement) {
    this.open = true;
  };
  HTMLDialogElement.prototype.close = function close(this: HTMLDialogElement) {
    this.open = false;
  };
}

export {};

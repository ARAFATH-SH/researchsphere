// saveBlob hands a file the app fetched to the browser to save, under name.
export function saveBlob(blob: Blob, name: string): void {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.rel = "noopener";
  a.style.display = "none";
  document.body.appendChild(a);
  a.click();
  a.remove();
  // Revoke after the click has been handled, or some browsers cancel the save.
  setTimeout(() => URL.revokeObjectURL(url), 0);
}

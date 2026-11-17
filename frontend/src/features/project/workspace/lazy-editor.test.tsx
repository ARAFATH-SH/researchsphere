import "@/test/codemirror";

import { render, screen } from "@testing-library/react";

import { getProject } from "../api";
import { getFile, listFiles } from "../files/api";
import { figure, folder, textFile, tree } from "../files/test-files";
import { project, projectID } from "../test-projects";
import { WorkspacePage } from "./workspace-page";

// The editor module, and CodeMirror with it, loads only when a text file opens (next/dynamic),
// so the workspace's first code and every other route stay without it.

const mockEditorLoaded = jest.fn();
jest.mock("./editor/latex-editor", () => {
  mockEditorLoaded();
  return jest.requireActual("./editor/latex-editor");
});
const mockCodeMirrorLoaded = jest.fn();
jest.mock("@codemirror/view", () => {
  mockCodeMirrorLoaded();
  return jest.requireActual("@codemirror/view");
});
jest.mock("../api", () => ({ ...jest.requireActual("../api"), getProject: jest.fn() }));
jest.mock("../files/api", () => ({ ...jest.requireActual("../files/api"), listFiles: jest.fn(), getFile: jest.fn() }));
jest.mock("@/features/moderation/warning-banner", () => ({ WarningBanner: () => null }));

const plot = figure("f-plot", "figures/plot.png");
const main = textFile("t-main", "main.tex");

it("loads the editor and CodeMirror only once a text file opens", async () => {
  jest.mocked(getProject).mockResolvedValue({ ok: true, data: project() });
  jest.mocked(getFile).mockImplementation(async (_p, id) =>
    id === plot.id
      ? { ok: true, data: { file: plot, content: null, download: { url: "https://storage.test/p", expiresAt: "2026-11-17T10:05:00Z" } } }
      : { ok: true, data: { file: main, content: "x", download: null } },
  );

  // A project with no text file: the workspace works without the editor.
  jest.mocked(listFiles).mockResolvedValue({ ok: true, data: tree([folder("d1", "figures"), plot]) });
  const first = render(<WorkspacePage projectId={projectID} />);
  expect(await screen.findByText("Choose a file to open it.")).toBeTruthy();
  expect(mockEditorLoaded).not.toHaveBeenCalled();
  expect(mockCodeMirrorLoaded).not.toHaveBeenCalled();
  first.unmount();

  jest.mocked(listFiles).mockResolvedValue({ ok: true, data: tree([main]) });
  render(<WorkspacePage projectId={projectID} />);
  expect(await screen.findByRole("textbox", { name: "Editing main.tex" })).toBeTruthy();
  expect(mockEditorLoaded).toHaveBeenCalledTimes(1);
  expect(mockCodeMirrorLoaded).toHaveBeenCalledTimes(1);
});

import { call } from "./call";

const refused = (error: object) => async () => ({ error: error as never, response: { ok: false } as never });

describe("call", () => {
  it("keeps the reason and end time of a refusal", async () => {
    const res = await call(refused({ code: "account_suspended", message: "No.", reason: "Spam", until: "2026-12-03T14:05:00Z" }));
    expect(res).toEqual({ ok: false, code: "account_suspended", message: "No.", reason: "Spam", until: "2026-12-03T14:05:00Z" });
  });

  it("keeps the compile a compile_running refusal names", async () => {
    const res = await call(refused({ code: "compile_running", message: "Busy.", compileId: "c-1" }));
    expect(res).toEqual({ ok: false, code: "compile_running", message: "Busy.", compileId: "c-1" });
  });

  it("adds nothing to a refusal that has neither", async () => {
    expect(await call(refused({ code: "forbidden", message: "No." }))).toEqual({ ok: false, code: "forbidden", message: "No." });
  });
});

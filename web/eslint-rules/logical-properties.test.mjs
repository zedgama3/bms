// @vitest-environment node
import { ESLint } from "eslint";
import { describe, expect, it } from "vitest";
import bms from "./logical-properties.js";

async function lint(code, file = "src/x.tsx") {
  const eslint = new ESLint({
    overrideConfigFile: true,
    overrideConfig: [
      {
        files: ["**/*.tsx"],
        languageOptions: { parserOptions: { ecmaFeatures: { jsx: true } } },
        plugins: { bms },
        rules: {
          "bms/logical-properties": "error",
          "bms/no-wide-min-width": "error",
          "bms/no-hover-disclosure": "error",
        },
      },
    ],
  });
  const [res] = await eslint.lintText(code, { filePath: file });
  return res.messages.map((m) => m.message);
}

describe("bms/logical-properties (decision 19)", () => {
  it.each([
    "ml-2",
    "mr-4",
    "pl-3",
    "pr-3",
    "left-0",
    "right-2",
    "text-left",
    "text-right",
    "border-l-2",
    "rounded-r-md",
    "sm:ml-2",
    "hover:pl-4",
    "-ml-2",
    "float-left",
  ])("reports %s", async (cls) => {
    expect((await lint(`const a = <div className="flex ${cls} gap-2" />;`)).length).toBe(1);
  });

  it.each([
    "ms-2",
    "me-4",
    "ps-3",
    "pe-3",
    "start-0",
    "end-2",
    "text-start",
    "text-end",
    "border-s-2",
    "rounded-e-md",
    "flex gap-2 min-h-11",
    "left-aligned prose",
  ])("accepts %s", async (cls) => {
    expect(await lint(`const a = <div className="${cls}" />;`)).toEqual([]);
  });

  it("looks inside template literals", async () => {
    expect((await lint("const a = <div className={`x ${y} ml-2`} />;")).length).toBe(1);
  });
});

describe("bms/no-wide-min-width (phone first)", () => {
  it.each(["min-w-96", "min-w-[400px]", "min-w-sm", "min-w-[30rem]", "min-w-xl", "flex min-w-100"])(
    "reports %s",
    async (cls) => {
      expect((await lint(`const a = <div className="${cls}" />;`)).length).toBe(1);
    },
  );
  it.each(["min-w-11", "min-w-xs", "min-w-[320px]", "sm:min-w-96", "md:min-w-[400px]", "min-w-0"])(
    "accepts %s",
    async (cls) => {
      expect(await lint(`const a = <div className="${cls}" />;`)).toEqual([]);
    },
  );
});

describe("bms/no-hover-disclosure (touch only)", () => {
  it.each(["group-hover:block", "hover:visible", "hover:flex", "group-hover:opacity-100"])(
    "reports %s",
    async (cls) => {
      expect((await lint(`const a = <div className="${cls}" />;`)).length).toBe(1);
    },
  );
  it.each(["hover:bg-surface", "hover:underline", "block"])("accepts %s", async (cls) => {
    expect(await lint(`const a = <div className="${cls}" />;`)).toEqual([]);
  });
});

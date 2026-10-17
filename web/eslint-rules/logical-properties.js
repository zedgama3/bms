// Decision 19: logical properties, not left/right. A class string with ml-, mr-, pl-, pr-,
// left-, right-, text-left, text-right, border-l/r, rounded-l/r ... is reported; use ms-, me-,
// ps-, pe-, start-, end-, text-start, text-end, border-s/e, rounded-s/e.
//
// The inset and spacing utilities (ml-, left-, ...) are only reported before a Tailwind
// value (a digit, [..], px, auto, full, a fraction), so ordinary English such as
// "left-aligned" is left alone.
const VALUE = String.raw`(?:\d|\[|px\b|auto\b|full\b)`;
const PHYSICAL = new RegExp(
  String.raw`(?:^|[\s:!])-?(?:(?:scroll-)?(?:m|p)(?:l|r)-${VALUE}|(?:left|right)-${VALUE}|text-(?:left|right)(?:\s|$)|float-(?:left|right)(?:\s|$)|border-(?:l|r)(?:-|\s|$)|rounded-(?:l|r|tl|tr|bl|br)(?:-|\s|$))`,
);

/** @type {import("eslint").Rule.RuleModule} */
const rule = {
  meta: {
    type: "suggestion",
    schema: [],
    messages: {
      physical:
        "Use a logical property (ms-/me-/ps-/pe-/start-/end-/text-start/text-end), not left/right: {{ found }}",
    },
  },
  create(context) {
    const check = (node, text) => {
      const m = PHYSICAL.exec(text);
      if (m) context.report({ node, messageId: "physical", data: { found: m[0].trim() } });
    };
    return {
      Literal(node) {
        if (typeof node.value === "string") check(node, node.value);
      },
      TemplateElement(node) {
        check(node, node.value.cooked ?? "");
      },
    };
  },
};

// Phone first (decision 21): no unprefixed min-width above 320 px on a layout container, and
// no content revealed by hover alone. Class tokens with a breakpoint or state prefix
// (sm:min-w-96) are fine: they apply only on wider screens or in a state.
const tokens = (text) => text.split(/\s+/).filter(Boolean);

const NAMED_MIN = {
  xs: 320,
  sm: 384,
  md: 448,
  lg: 512,
  xl: 576,
  "2xl": 672,
  "3xl": 768,
  "4xl": 896,
  "5xl": 1024,
  "6xl": 1152,
  "7xl": 1280,
};

/** Pixels of an unprefixed min-w-* token, or 0 when it is not wider than a phone could be. */
function minWidthPx(token) {
  const m = /^-?min-w-(.+)$/.exec(token);
  if (!m) return 0;
  const v = m[1];
  const px = /^\[(\d+(?:\.\d+)?)px\]$/.exec(v);
  if (px) return Number(px[1]);
  const rem = /^\[(\d+(?:\.\d+)?)rem\]$/.exec(v);
  if (rem) return Number(rem[1]) * 16;
  if (/^\d+(\.\d+)?$/.test(v)) return Number(v) * 4;
  if (v in NAMED_MIN) return NAMED_MIN[v];
  return 0;
}

/** @type {import("eslint").Rule.RuleModule} */
const wideMinWidth = {
  meta: {
    type: "problem",
    schema: [],
    messages: {
      wide: "min-width {{ found }} is wider than a 320 px phone; use a breakpoint prefix (sm:{{ found }})",
    },
  },
  create(context) {
    const check = (node, text) => {
      for (const t of tokens(text)) {
        if (!t.includes(":") && minWidthPx(t) > 320)
          context.report({ node, messageId: "wide", data: { found: t } });
      }
    };
    return {
      Literal(node) {
        if (typeof node.value === "string") check(node, node.value);
      },
      TemplateElement(node) {
        check(node, node.value.cooked ?? "");
      },
    };
  },
};

const HOVER_REVEAL =
  /^(?:group-hover|peer-hover|hover):(?:block|flex|grid|inline|inline-block|inline-flex|visible|opacity-(?:[1-9]\d*))$/;

/** @type {import("eslint").Rule.RuleModule} */
const hoverDisclosure = {
  meta: {
    type: "problem",
    schema: [],
    messages: { hover: "Content revealed by hover alone is unreachable by touch: {{ found }}" },
  },
  create(context) {
    const check = (node, text) => {
      for (const t of tokens(text))
        if (HOVER_REVEAL.test(t)) context.report({ node, messageId: "hover", data: { found: t } });
    };
    return {
      Literal(node) {
        if (typeof node.value === "string") check(node, node.value);
      },
      TemplateElement(node) {
        check(node, node.value.cooked ?? "");
      },
    };
  },
};

export default {
  rules: {
    "logical-properties": rule,
    "no-wide-min-width": wideMinWidth,
    "no-hover-disclosure": hoverDisclosure,
  },
};
export { PHYSICAL, minWidthPx };

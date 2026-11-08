export const MAX_NAME_LENGTH = 200;
export const MAX_TIP_LENGTH = 500;

const DOT = "[.\\u{3002}\\u{FF0E}\\u{FF61}\\u{FE52}]";
const LABEL = "[\\p{L}\\p{M}\\p{N}-]+";
const DOMAIN = new RegExp(`${LABEL}(?:${DOT}${LABEL})*${DOT}\\p{L}[\\p{L}\\p{M}]+`, "gu");
const IPV4 = new RegExp(`[0-9]{1,3}(?:${DOT}[0-9]{1,3}){3}`, "gu");
const PHONE = /\p{Nd}(?:[\p{Zs}\p{Pd}()./~\u{2212}]*\p{Nd}){9,}/u;
const NOT_GRAPHIC = /[^\p{L}\p{M}\p{N}\p{P}\p{S}\p{Zs}]/u;
const DOTS = new RegExp(DOT, "gu");
const ONE_DOT_LEADER = String.fromCodePoint(0x2024);

export const codePoints = (text: string): number => [...text].length;

const matches = (re: RegExp, text: string): boolean => {
  re.lastIndex = 0;
  return re.test(text);
};

export const isSafeText = (text: string): boolean =>
  !text.includes("://") &&
  !NOT_GRAPHIC.test(text) &&
  !matches(DOMAIN, text) &&
  !matches(IPV4, text) &&
  !PHONE.test(text);

const isValidField = (text: string, maxLength: number): boolean =>
  text.trim() !== "" && codePoints(text) <= maxLength && isSafeText(text);

export const isValidName = (name: string): boolean => isValidField(name, MAX_NAME_LENGTH);

export const isValidTip = (tip: string): boolean => tip === "" || isValidField(tip, MAX_TIP_LENGTH);

const defuseOnce = (text: string): string =>
  text
    .replace(DOMAIN, (m) => m.replace(DOTS, ONE_DOT_LEADER))
    .replace(IPV4, (m) => m.replace(DOTS, ONE_DOT_LEADER));

export function defuseLinks(text: string): string {
  let current = text;
  for (let next = defuseOnce(current); next !== current; next = defuseOnce(current)) current = next;
  return current;
}

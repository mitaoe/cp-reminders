export const MAX_TIP_LENGTH = 500;

const DOMAIN = /[\p{L}\p{N}-]+(?:[.\u{3002}\u{FF0E}\u{FF61}][\p{L}\p{N}-]+)*[.\u{3002}\u{FF0E}\u{FF61}]\p{L}{2,}/giu;
const IPV4 = /[0-9]{1,3}(?:[.\u{3002}\u{FF0E}\u{FF61}][0-9]{1,3}){3}/gu;
const PHONE = /[0-9](?:[ ()\-.]*[0-9]){9,}/u;
const NOT_GRAPHIC = /[^\p{L}\p{M}\p{N}\p{P}\p{S}\p{Zs}]/u;
const DOTS = /[.\u{3002}\u{FF0E}\u{FF61}]/gu;
const ONE_DOT_LEADER = String.fromCodePoint(0x2024);

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

export const isValidTip = (tip: string): boolean =>
  tip === "" || (tip.length <= MAX_TIP_LENGTH && tip.trim() !== "" && isSafeText(tip));

export const defuseLinks = (text: string): string =>
  text
    .replace(DOMAIN, (m) => m.replace(DOTS, ONE_DOT_LEADER))
    .replace(IPV4, (m) => m.replace(DOTS, ONE_DOT_LEADER));

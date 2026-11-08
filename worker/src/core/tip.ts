export const MAX_TIP_LENGTH = 500;

const SCHEME_LINK = /\b[a-z][a-z0-9+.-]*:\/\/\S*/iu;
const BARE_DOMAIN = /[\p{L}\p{N}-]+(?:\.[\p{L}\p{N}-]+)*\.\p{L}{2,}/iu;
const LINE_BREAK = /[\r\n\p{Zl}\p{Zp}]/u;

export const hasLink = (text: string): boolean => SCHEME_LINK.test(text) || BARE_DOMAIN.test(text);

export const isValidTip = (tip: string): boolean =>
  tip.length <= MAX_TIP_LENGTH && !LINE_BREAK.test(tip) && !hasLink(tip);

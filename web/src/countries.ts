// Common Google markets: [country, language, label]. DataFluxor supports more;
// any two-letter country can be typed in.
export const MARKETS: [string, string, string][] = [
  ["US", "en", "United States"],
  ["GB", "en", "United Kingdom"],
  ["CA", "en", "Canada"],
  ["AU", "en", "Australia"],
  ["IE", "en", "Ireland"],
  ["NZ", "en", "New Zealand"],
  ["IN", "en", "India"],
  ["ZA", "en", "South Africa"],
  ["SG", "en", "Singapore"],
  ["DE", "de", "Germany"],
  ["AT", "de", "Austria"],
  ["CH", "de", "Switzerland"],
  ["FR", "fr", "France"],
  ["BE", "nl", "Belgium"],
  ["NL", "nl", "Netherlands"],
  ["ES", "es", "Spain"],
  ["MX", "es", "Mexico"],
  ["AR", "es", "Argentina"],
  ["IT", "it", "Italy"],
  ["PT", "pt", "Portugal"],
  ["BR", "pt", "Brazil"],
  ["SE", "sv", "Sweden"],
  ["NO", "no", "Norway"],
  ["DK", "da", "Denmark"],
  ["FI", "fi", "Finland"],
  ["PL", "pl", "Poland"],
  ["JP", "ja", "Japan"],
  ["AE", "en", "United Arab Emirates"],
];

export function languageFor(country: string) {
  return MARKETS.find((m) => m[0] === country)?.[1] ?? "en";
}

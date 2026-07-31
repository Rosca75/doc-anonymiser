// countries.js, the document-country table (BUILD-05 Phase 5, decision 2).
//
// The Identify rail's country selector does exactly TWO things, and it is worth
// being precise about how little that is:
//
//   1. It swaps the EXAMPLE STRINGS beside three category labels (phone, VAT,
//      national identification), so a user working on a French document sees
//      "+33 6 12 34 56 78" rather than a Luxembourg number.
//   2. It switches the three country-specific ID categories on or off
//      (de_steuer_id, es_nif, uk_nhs), because a German tax number is worth
//      looking for in a German document and is noise in a French one.
//
// That is all. There is deliberately no locale-aware engine behind it: no new
// regexes, no per-country detection, and engine/pii.go is untouched. The
// recognizers are already international; the country only decides which of the
// national ones are worth switching on and how the examples read.
//
// This module is PURE (no DOM, no state, no bridge), so countries.test.js
// exercises it directly.

// COUNTRIES is the table. Five entries, matching the mock-up.
//
// `ids` lists the country-specific ID categories that country turns ON. An
// empty list is meaningful rather than missing: Luxembourg and France have no
// dedicated recognizer among the three, so picking them turns all three OFF.
//
// The example strings are FORMAT illustrations, not real identifiers. They are
// chosen to show the shape a user should recognise (country prefix, digit
// grouping, a trailing letter where there is one), because "13 digit number" on
// its own does not tell anyone whether their document's numbers qualify.
export const COUNTRIES = [
  {
    code: "LU", name: "Luxembourg",
    phone: "+352 621 123 456",
    vat: "LU12345678",
    matricule: "the 13 digit national number",
    ids: [],
  },
  {
    code: "FR", name: "France",
    phone: "+33 6 12 34 56 78",
    vat: "FR90887654",
    matricule: "the 15 digit NIR",
    ids: [],
  },
  {
    code: "DE", name: "Germany",
    phone: "+49 151 234 5678",
    vat: "DE123456789",
    matricule: "the 11 digit Steueridentifikationsnummer",
    ids: ["de_steuer_id"],
  },
  {
    code: "ES", name: "Spain",
    phone: "+34 612 345 678",
    vat: "ESA12345674",
    matricule: "the NIF, 8 digits and a letter",
    ids: ["es_nif"],
  },
  {
    code: "UK", name: "United Kingdom",
    phone: "+44 7700 900123",
    vat: "GB123456789",
    matricule: "the National Insurance number, for example QQ123456C",
    ids: ["uk_nhs"],
  },
];

// DEFAULT_COUNTRY is Luxembourg: the application replaces two internal
// notebooks used on Luxembourg engagements, so it is the common case rather
// than an arbitrary first entry.
export const DEFAULT_COUNTRY = "LU";

// COUNTRY_ID_CATEGORIES are the three engine categories the selector switches.
//
// They are listed here as WELL as inside the table entries because the reducer
// needs to know the full set to switch OFF: picking France has to clear
// Germany's and Spain's, and it can only do that if it knows what "all of them"
// is. Every key must exist in state.js ALL_CATEGORIES, which countries.test.js
// checks, because a switch for a category the engine does not know does nothing
// at all.
export const COUNTRY_ID_CATEGORIES = ["de_steuer_id", "es_nif", "uk_nhs"];

/**
 * countryFor(code) returns the table entry, falling back to the default rather
 * than returning undefined: an unknown code (a hand-edited session, a future
 * country removed from the table) must leave the rail rendering, not blank.
 * @param {string} code an ISO-ish two-letter code from the selector
 * @returns {object} a COUNTRIES entry
 */
export function countryFor(code) {
  return COUNTRIES.find((c) => c.code === code) ??
    COUNTRIES.find((c) => c.code === DEFAULT_COUNTRY) ??
    COUNTRIES[0];
}

/**
 * examplesFor(code) returns the three example strings that depend on the
 * country, keyed by ENGINE CATEGORY so the rail can merge them straight over
 * copy.js CATEGORY_LABELS.
 *
 * Only three categories are country-dependent. Every other recognizer either
 * has one international format (an email address, an IBAN, an IP address) or a
 * format that does not vary by the document's country (a payment card).
 *
 * @param {string} code a country code
 * @returns {Record<string,string>} category key to example string
 */
export function examplesFor(code) {
  const country = countryFor(code);
  return {
    phone: country.phone,
    vat: country.vat,
    matricule: country.matricule,
  };
}

/**
 * countryIDCategories(code) returns the on/off state of the three
 * country-specific ID categories for a country: true for the ones that country
 * uses, false for the rest.
 *
 * It returns the WHOLE set rather than only the ones to switch on, so the
 * caller applies one complete answer instead of having to work out what to
 * clear. That is what makes switching from Germany to France turn the German
 * tax ID off rather than leaving it on alongside nothing.
 *
 * @param {string} code a country code
 * @returns {Record<string,boolean>} category key to whether it applies
 */
export function countryIDCategories(code) {
  const country = countryFor(code);
  const out = {};
  for (const key of COUNTRY_ID_CATEGORIES) {
    out[key] = country.ids.includes(key);
  }
  return out;
}

/**
 * countryOptions() is the selector's option list: code and visible name only,
 * so a view never has to know the table's shape.
 * @returns {Array<{code: string, name: string}>}
 */
export function countryOptions() {
  return COUNTRIES.map((c) => ({ code: c.code, name: c.name }));
}

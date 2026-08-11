// countries.js, the document-country table (BUILD-05 Phase 5, decision 2).
//
// The Identify rail's country selector now mirrors the engine's country model.
// It still swaps the example strings, but it also reflects which regex
// categories apply to which countries so the frontend and Go cannot drift.
//
//   1. It swaps the EXAMPLE STRINGS beside three category labels (phone, VAT,
//      national identification), so a user working on a French document sees
//      "+33 6 12 34 56 78" rather than a Luxembourg number.
//   2. It switches the country-scoped regex categories on or off, because a
//      German tax number is worth looking for in a German document and is noise
//      in a French one.
//
// The engine owns the same table in backend/engine/country.go. This file is its
// JS mirror for rendering and local reducers.
//
// This module is PURE (no DOM, no state, no bridge), so countries.test.js
// exercises it directly.

// COUNTRIES is the table. Five entries, matching the mock-up.
//
// `ids` lists the country-scoped categories that country turns ON.
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
    ids: ["matricule"],
  },
  {
    code: "FR", name: "France",
    phone: "+33 6 12 34 56 78",
    vat: "FR40303265045",
    matricule: "no dedicated French national ID detector in this version",
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

// CATEGORY_COUNTRIES mirrors backend/engine/country.go CategoryCountries. A nil
// Go slice becomes "all five countries listed explicitly" here so the rail can
// name the allowed set in tooltips without special-casing.
export const CATEGORY_COUNTRIES = {
  email: ["LU", "FR", "DE", "ES", "UK"],
  url: ["LU", "FR", "DE", "ES", "UK"],
  iban: ["LU", "FR", "DE", "ES", "UK"],
  vat: ["LU", "FR", "DE", "ES", "UK"],
  matricule: ["LU"],
  phone: ["LU", "FR", "DE", "ES", "UK"],
  amount: ["LU", "FR", "DE", "ES", "UK"],
  date: ["LU", "FR", "DE", "ES", "UK"],
  credit_card: ["LU", "FR", "DE", "ES", "UK"],
  uk_nhs: ["UK"],
  ip_address: ["LU", "FR", "DE", "ES", "UK"],
  mac_address: ["LU", "FR", "DE", "ES", "UK"],
  crypto: ["LU", "FR", "DE", "ES", "UK"],
  database_uri: ["LU", "FR", "DE", "ES", "UK"],
  de_steuer_id: ["DE"],
  es_nif: ["ES"],
};

export const COUNTRY_ID_CATEGORIES = ["matricule", "de_steuer_id", "es_nif", "uk_nhs"];

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
  const out = {};
  for (const key of COUNTRY_ID_CATEGORIES) {
    out[key] = (CATEGORY_COUNTRIES[key] ?? []).includes(countryFor(code).code);
  }
  return out;
}

export function categoryAppliesTo(category, code) {
  const countries = CATEGORY_COUNTRIES[category];
  if (!countries) return true;
  return countries.includes(countryFor(code).code);
}

/**
 * countryOptions() is the selector's option list: code and visible name only,
 * so a view never has to know the table's shape.
 * @returns {Array<{code: string, name: string}>}
 */
export function countryOptions() {
  return COUNTRIES.map((c) => ({ code: c.code, name: c.name }));
}

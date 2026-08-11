// countries.test.js, the document-country table (BUILD-05 Phase 5, decision 2).
//
// The table is small and the temptation is to leave it untested. The reason not
// to: it decides which of three engine categories are ACTIVE, and a key that
// drifts out of sync with state.js would be a switch that does nothing at all,
// which is exactly the class of bug category_parity_test.go was written for.

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  COUNTRIES, DEFAULT_COUNTRY, COUNTRY_ID_CATEGORIES, CATEGORY_COUNTRIES,
  countryFor, examplesFor, countryIDCategories, countryOptions, categoryAppliesTo,
} from "./countries.js";
import { ALL_CATEGORIES } from "./state.js";

// --- The table's own shape ----------------------------------------------

test("every country has a code, a name and all three examples", () => {
  assert.ok(COUNTRIES.length >= 5, "the mock-up shows five countries");
  for (const c of COUNTRIES) {
    for (const field of ["code", "name", "phone", "vat", "matricule"]) {
      assert.equal(typeof c[field], "string", `${c.code}: ${field}`);
      assert.ok(c[field].trim().length > 0, `${c.code}: ${field} is empty`);
    }
    assert.ok(Array.isArray(c.ids), `${c.code}: ids must be a list`);
  }
});

test("country codes are unique", () => {
  const codes = COUNTRIES.map((c) => c.code);
  assert.equal(new Set(codes).size, codes.length, `duplicate code in ${codes.join(", ")}`);
});

test("the default country is in the table", () => {
  assert.ok(COUNTRIES.some((c) => c.code === DEFAULT_COUNTRY),
    `${DEFAULT_COUNTRY} must be one of the entries, it is what every fallback lands on`);
});

// --- Parity with the engine's categories -------------------------------

test("every country-specific ID category is one the engine knows", () => {
  // A switch for a category no pass reads does nothing at all: the same bug
  // BUILD-04 CR9 fixed for the extended recognizers.
  for (const key of COUNTRY_ID_CATEGORIES) {
    assert.ok(ALL_CATEGORIES.includes(key),
      `${key} is not in state.js ALL_CATEGORIES, so switching it would do nothing`);
  }
});

test("every category-country row is one the engine knows", () => {
  for (const [category, countries] of Object.entries(CATEGORY_COUNTRIES)) {
    assert.ok(ALL_CATEGORIES.includes(category), `${category} is not in state.js ALL_CATEGORIES`);
    for (const code of countries) {
      assert.ok(COUNTRIES.some((c) => c.code === code), `${category} names unknown country ${code}`);
    }
  }
});

test("no country claims an identifier outside the switched set", () => {
  // A country listing an id that COUNTRY_ID_CATEGORIES does not cover would be
  // switched on and then never switched off again by another country.
  for (const c of COUNTRIES) {
    for (const id of c.ids) {
      assert.ok(COUNTRY_ID_CATEGORIES.includes(id),
        `${c.code} claims ${id}, which nothing would ever switch back off`);
    }
  }
});

test("each country-specific identifier belongs to exactly one country", () => {
  // Not a hard requirement of the model, but it is what the three recognizers
  // actually are (a German tax id, a Spanish one, a UK health number), and a
  // second claimant would mean the table had drifted from reality.
  for (const id of COUNTRY_ID_CATEGORIES) {
    const owners = COUNTRIES.filter((c) => c.ids.includes(id)).map((c) => c.code);
    assert.equal(owners.length, 1, `${id} is claimed by ${owners.join(", ") || "nobody"}`);
  }
});

// --- countryFor ---------------------------------------------------------

test("countryFor finds a country by code", () => {
  assert.equal(countryFor("DE").name, "Germany");
  assert.equal(countryFor("LU").code, "LU");
});

test("countryFor falls back to the default rather than returning undefined", () => {
  // A hand-edited session or a removed table entry must leave the rail
  // rendering, not blank.
  for (const bad of ["ZZ", "", undefined, null, "de"]) {
    assert.equal(countryFor(bad).code, DEFAULT_COUNTRY, JSON.stringify(bad));
  }
});

// --- examplesFor --------------------------------------------------------

test("examplesFor returns exactly the three country-dependent categories", () => {
  const examples = examplesFor("FR");
  assert.deepEqual(Object.keys(examples).sort(), ["matricule", "phone", "vat"]);
  // Keyed by ENGINE category, so the rail can merge them straight over
  // copy.js CATEGORY_LABELS.
  assert.match(examples.phone, /\+33/);
  assert.match(examples.vat, /^FR/);
});

test("examplesFor gives every country a distinct phone and VAT example", () => {
  // Identical examples would mean the selector visibly did nothing for at least
  // one choice, which reads as a broken control.
  const phones = COUNTRIES.map((c) => examplesFor(c.code).phone);
  const vats = COUNTRIES.map((c) => examplesFor(c.code).vat);
  assert.equal(new Set(phones).size, COUNTRIES.length, "two countries share a phone example");
  assert.equal(new Set(vats).size, COUNTRIES.length, "two countries share a VAT example");
});

test("examplesFor falls back with the country rather than throwing", () => {
  assert.deepEqual(examplesFor("ZZ"), examplesFor(DEFAULT_COUNTRY));
});

// --- countryIDCategories ------------------------------------------------

test("countryIDCategories answers for ALL three, not only the ones that apply", () => {
  // Returning the whole set is what makes switching from Germany to France turn
  // the German identifier OFF rather than leaving it on beside nothing.
  const german = countryIDCategories("DE");
  assert.deepEqual(Object.keys(german).sort(), [...COUNTRY_ID_CATEGORIES].sort());
  assert.equal(german.de_steuer_id, true);
  assert.equal(german.es_nif, false);
  assert.equal(german.uk_nhs, false);
});

test("France keeps only the categories that apply there", () => {
  const applies = countryIDCategories("FR");
  assert.equal(applies.matricule, false);
  assert.equal(applies.de_steuer_id, false);
  assert.equal(applies.es_nif, false);
  assert.equal(applies.uk_nhs, false);
});

test("Luxembourg keeps only its own national identifier", () => {
  const applies = countryIDCategories("LU");
  assert.equal(applies.matricule, true);
  assert.equal(applies.de_steuer_id, false);
  assert.equal(applies.es_nif, false);
  assert.equal(applies.uk_nhs, false);
});

test("categoryAppliesTo mirrors the country table", () => {
  for (const code of COUNTRIES.map((c) => c.code)) {
    for (const key of COUNTRY_ID_CATEGORIES) {
      assert.equal(categoryAppliesTo(key, code), countryIDCategories(code)[key], `${key}/${code}`);
    }
  }
});

test("a country with no dedicated foreign identifiers turns those off", () => {
  for (const code of ["FR"]) {
    const applies = countryIDCategories(code);
    for (const key of ["de_steuer_id", "es_nif", "uk_nhs"]) {
      assert.equal(applies[key], false, `${code} must not switch ${key} on`);
    }
  }
});

test("Spain and the United Kingdom each switch on their own identifier", () => {
  assert.equal(countryIDCategories("ES").es_nif, true);
  assert.equal(countryIDCategories("ES").de_steuer_id, false);
  assert.equal(countryIDCategories("UK").uk_nhs, true);
  assert.equal(countryIDCategories("UK").es_nif, false);
});

// --- countryOptions -----------------------------------------------------

test("countryOptions exposes only what a selector needs", () => {
  const options = countryOptions();
  assert.equal(options.length, COUNTRIES.length);
  for (const option of options) {
    assert.deepEqual(Object.keys(option).sort(), ["code", "name"],
      "a view must not have to know the table's shape");
  }
});

// engine/country.go — country-scoped regex category applicability.
//
// The country selector is no longer only copy on the frontend. The engine owns
// which regex categories apply to which countries, so the same answer drives
// pass 1, settings validation and the frontend mirror.
package engine

import "sort"

const (
	CountryLU = "LU"
	CountryFR = "FR"
	CountryDE = "DE"
	CountryES = "ES"
	CountryUK = "UK"
)

var SupportedCountries = []string{CountryLU, CountryFR, CountryDE, CountryES, CountryUK}

// CategoryCountries maps a regex category to the countries it applies to.
// nil means "all supported countries".
var CategoryCountries = map[string][]string{
	CatEmail:       nil,
	CatURL:         nil,
	CatIBAN:        nil,
	CatVAT:         nil,
	CatMatricule:   {CountryLU},
	CatPhone:       nil,
	CatAmount:      nil,
	CatDate:        nil,
	CatCreditCard:  nil,
	CatNHS:         {CountryUK},
	CatIPAddress:   nil,
	CatMACAddress:  nil,
	CatCrypto:      nil,
	CatDatabaseURI: nil,
	CatDESteuerID:  {CountryDE},
	CatESNIF:       {CountryES},
	// A BIC is ISO 9362 and carries its own country code, so it applies
	// everywhere.
	CatBIC: nil,
	// A postal code is a national SHAPE. The only shape implemented is the
	// Luxembourg "L-" plus four digits, and a bare four-digit run is an ordinary
	// number in every other country, so the category is scoped rather than
	// running a pattern that would fire on clause numbers.
	CatPostalCode: {CountryLU},
	// An address line is anchored on a street-type gazetteer, and the gazetteer
	// implemented is the French/Luxembourg one. A category outside the selected
	// country renders DISABLED rather than hidden, so a German selection shows
	// the switch and says it does not apply here.
	CatAddress: {CountryLU, CountryFR},
}

func CategoryAppliesTo(category, country string) bool {
	allowed, ok := CategoryCountries[category]
	if !ok {
		return false
	}
	if len(allowed) == 0 {
		return true
	}
	for _, code := range allowed {
		if code == country {
			return true
		}
	}
	return false
}

func KnownCountry(code string) bool {
	for _, country := range SupportedCountries {
		if country == code {
			return true
		}
	}
	return false
}

func CategoryCountriesCopy() map[string][]string {
	out := make(map[string][]string, len(CategoryCountries))
	for key, countries := range CategoryCountries {
		if len(countries) == 0 {
			out[key] = nil
			continue
		}
		copyCountries := append([]string(nil), countries...)
		sort.Strings(copyCountries)
		out[key] = copyCountries
	}
	return out
}

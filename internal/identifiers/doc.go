// Package identifiers creates entity identifiers and converts them between
// their stored and exposed forms. Every entity id is a UUID version 7. The API
// exposes it as a type prefix, an underscore and the 26 character lowercase
// Crockford base32 form of the 128-bit value, such as
// cust_01jbvagescfn78y0938nkrkayd.
package identifiers

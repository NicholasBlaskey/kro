// Copyright 2025 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package analysis

// CELFunction represents a CEL function with its signature and documentation
type CELFunction struct {
	Name        string
	Signature   string
	Returns     string
	Description string
	Example     string
	Category    string
}

// CELFunctions is the catalog of all CEL functions available in Kro
var CELFunctions = []CELFunction{
	// Kro Custom Libraries
	{
		Name:        "hash.fnv64a",
		Signature:   "hash.fnv64a(value: string) -> bytes",
		Returns:     "bytes",
		Description: "Computes the FNV-1a 64-bit hash of a string. FNV-1a is a fast, non-cryptographic hash function suitable for checksums, identifiers, and cache keys. This is the recommended hash function for all use cases in kro.",
		Example:     `base64.encode(hash.fnv64a('hello world'))`,
		Category:    "kro-hash",
	},
	{
		Name:        "hash.sha256",
		Signature:   "hash.sha256(value: string) -> bytes",
		Returns:     "bytes",
		Description: "Computes the SHA-256 hash of a string. Provided for backwards compatibility. Use hash.fnv64a() for new code unless you have a specific requirement for SHA-256.",
		Example:     `hash.sha256(schema.spec.data).base64()`,
		Category:    "kro-hash",
	},
	{
		Name:        "hash.md5",
		Signature:   "hash.md5(value: string) -> bytes",
		Returns:     "bytes",
		Description: "Computes the MD5 hash of a string. Provided for backwards compatibility. Use hash.fnv64a() for new code unless you have a specific requirement for MD5.",
		Example:     `base64.encode(hash.md5('hello world'))`,
		Category:    "kro-hash",
	},
	{
		Name:        "random.seededString",
		Signature:   "random.seededString(length: int, seed: string) -> string",
		Returns:     "string",
		Description: "Generates a deterministic random alphanumeric string of the given length using the seed. Same length and seed always produce the same string.",
		Example:     `random.seededString(10, schema.metadata.uid)`,
		Category:    "kro-random",
	},
	{
		Name:        "random.seededInt",
		Signature:   "random.seededInt(min: int, max: int, seed: string) -> int",
		Returns:     "int",
		Description: "Generates a deterministic random integer in [min, max) using the seed. Same min, max, and seed always produce the same integer.",
		Example:     `random.seededInt(30000, 32768, schema.metadata.uid)`,
		Category:    "kro-random",
	},
	{
		Name:        "omit",
		Signature:   "omit() -> kro.omit",
		Returns:     "kro.omit",
		Description: "Returns a sentinel value that removes a field (or array element) from the rendered object. Only valid in standalone template field expressions. Must not be used in includeWhen, readyWhen, forEach, or string template fragments.",
		Example:     `policy: ${schema.spec.policy != "" ? schema.spec.policy : omit()}`,
		Category:    "kro-omit",
	},
	{
		Name:        "json.unmarshal",
		Signature:   "json.unmarshal(jsonString: string) -> dyn",
		Returns:     "dyn",
		Description: "Parses a JSON string and returns the parsed value. Returns map for JSON objects, list for JSON arrays, or primitives (string, int, double, bool) for JSON values.",
		Example:     `json.unmarshal('{"name": "test"}').name`,
		Category:    "kro-json",
	},
	{
		Name:        "json.marshal",
		Signature:   "json.marshal(value: dyn) -> string",
		Returns:     "string",
		Description: "Converts a CEL value to a JSON string. Accepts any CEL value (map, list, string, number, bool, null).",
		Example:     `json.marshal({"name": "test", "count": 42})`,
		Category:    "kro-json",
	},
	{
		Name:        "lists.setAtIndex",
		Signature:   "lists.setAtIndex(list: list(T), index: int, value: T) -> list(T)",
		Returns:     "list(T)",
		Description: "Returns a new list with the element at index replaced by value. Index must be in [0, size(list)). Does not modify the input list.",
		Example:     `lists.setAtIndex([1, 2, 3], 1, 99)  // [1, 99, 3]`,
		Category:    "kro-lists",
	},
	{
		Name:        "lists.insertAtIndex",
		Signature:   "lists.insertAtIndex(list: list(T), index: int, value: T) -> list(T)",
		Returns:     "list(T)",
		Description: "Returns a new list with value inserted before the element at index. Index must be in [0, size(list)]. An index equal to size(list) appends. Does not modify the input list.",
		Example:     `lists.insertAtIndex([1, 2, 3], 1, 99)  // [1, 99, 2, 3]`,
		Category:    "kro-lists",
	},
	{
		Name:        "lists.removeAtIndex",
		Signature:   "lists.removeAtIndex(list: list(T), index: int) -> list(T)",
		Returns:     "list(T)",
		Description: "Returns a new list with the element at index removed. Index must be in [0, size(list)). Does not modify the input list.",
		Example:     `lists.removeAtIndex([1, 2, 3], 1)  // [1, 3]`,
		Category:    "kro-lists",
	},
	{
		Name:        "merge",
		Signature:   "map(string, T).merge(other: map(string, T)) -> map(string, T)",
		Returns:     "map(string, T)",
		Description: "Merges two maps. Keys from the second map overwrite already available keys in the first map. Keys must be of type string, value types must be identical in the maps merged.",
		Example:     `{'a': 1}.merge({'b': 2})  // {'a': 1, 'b': 2}`,
		Category:    "kro-maps",
	},

	// Standard CEL String Functions (from ext.Strings)
	{
		Name:        "contains",
		Signature:   "string.contains(substring: string) -> bool",
		Returns:     "bool",
		Description: "Returns true if the string contains the given substring.",
		Example:     `'hello world'.contains('world')  // true`,
		Category:    "string",
	},
	{
		Name:        "startsWith",
		Signature:   "string.startsWith(prefix: string) -> bool",
		Returns:     "bool",
		Description: "Returns true if the string starts with the given prefix.",
		Example:     `'hello'.startsWith('he')  // true`,
		Category:    "string",
	},
	{
		Name:        "endsWith",
		Signature:   "string.endsWith(suffix: string) -> bool",
		Returns:     "bool",
		Description: "Returns true if the string ends with the given suffix.",
		Example:     `'hello'.endsWith('lo')  // true`,
		Category:    "string",
	},
	{
		Name:        "matches",
		Signature:   "string.matches(pattern: string) -> bool",
		Returns:     "bool",
		Description: "Returns true if the string matches the given regular expression pattern.",
		Example:     `'hello123'.matches('[a-z]+[0-9]+')  // true`,
		Category:    "string",
	},
	{
		Name:        "split",
		Signature:   "string.split(separator: string) -> list(string)",
		Returns:     "list(string)",
		Description: "Splits the string by the given separator and returns a list of substrings.",
		Example:     `'a,b,c'.split(',')  // ['a', 'b', 'c']`,
		Category:    "string",
	},
	{
		Name:        "join",
		Signature:   "list(string).join(separator: string) -> string",
		Returns:     "string",
		Description: "Joins a list of strings with the given separator.",
		Example:     `['a', 'b', 'c'].join(',')  // 'a,b,c'`,
		Category:    "string",
	},
	{
		Name:        "replace",
		Signature:   "string.replace(old: string, new: string) -> string",
		Returns:     "string",
		Description: "Replaces all occurrences of old substring with new substring.",
		Example:     `'hello world'.replace('world', 'universe')`,
		Category:    "string",
	},
	{
		Name:        "trim",
		Signature:   "string.trim() -> string",
		Returns:     "string",
		Description: "Removes leading and trailing whitespace from the string.",
		Example:     `'  hello  '.trim()  // 'hello'`,
		Category:    "string",
	},
	{
		Name:        "lowerAscii",
		Signature:   "string.lowerAscii() -> string",
		Returns:     "string",
		Description: "Converts all ASCII characters in the string to lowercase.",
		Example:     `'HELLO'.lowerAscii()  // 'hello'`,
		Category:    "string",
	},
	{
		Name:        "upperAscii",
		Signature:   "string.upperAscii() -> string",
		Returns:     "string",
		Description: "Converts all ASCII characters in the string to uppercase.",
		Example:     `'hello'.upperAscii()  // 'HELLO'`,
		Category:    "string",
	},
	{
		Name:        "substring",
		Signature:   "string.substring(start: int, end: int) -> string",
		Returns:     "string",
		Description: "Returns a substring from start index (inclusive) to end index (exclusive).",
		Example:     `'hello'.substring(1, 4)  // 'ell'`,
		Category:    "string",
	},

	// Standard CEL List Functions (from ext.Lists)
	{
		Name:        "size",
		Signature:   "size(list | map | string) -> int",
		Returns:     "int",
		Description: "Returns the number of elements in a list, map, or characters in a string.",
		Example:     `size([1, 2, 3])  // 3`,
		Category:    "list",
	},
	{
		Name:        "filter",
		Signature:   "list(T).filter(predicate: T -> bool) -> list(T)",
		Returns:     "list(T)",
		Description: "Returns a new list containing only elements for which the predicate returns true.",
		Example:     `[1, 2, 3, 4].filter(x, x > 2)  // [3, 4]`,
		Category:    "list",
	},
	{
		Name:        "map",
		Signature:   "list(T).map(transform: T -> U) -> list(U)",
		Returns:     "list(U)",
		Description: "Returns a new list with each element transformed by the given function.",
		Example:     `[1, 2, 3].map(x, x * 2)  // [2, 4, 6]`,
		Category:    "list",
	},
	{
		Name:        "all",
		Signature:   "list(T).all(predicate: T -> bool) -> bool",
		Returns:     "bool",
		Description: "Returns true if the predicate is true for all elements in the list.",
		Example:     `[1, 2, 3].all(x, x > 0)  // true`,
		Category:    "list",
	},
	{
		Name:        "exists",
		Signature:   "list(T).exists(predicate: T -> bool) -> bool",
		Returns:     "bool",
		Description: "Returns true if the predicate is true for at least one element in the list.",
		Example:     `[1, 2, 3].exists(x, x > 2)  // true`,
		Category:    "list",
	},
	{
		Name:        "exists_one",
		Signature:   "list(T).exists_one(predicate: T -> bool) -> bool",
		Returns:     "bool",
		Description: "Returns true if the predicate is true for exactly one element in the list.",
		Example:     `[1, 2, 3].exists_one(x, x == 2)  // true`,
		Category:    "list",
	},

	// Encoding Functions (from ext.Encoders)
	{
		Name:        "base64.encode",
		Signature:   "base64.encode(value: bytes) -> string",
		Returns:     "string",
		Description: "Encodes bytes to a base64 string.",
		Example:     `base64.encode(b'hello')`,
		Category:    "encoding",
	},
	{
		Name:        "base64.decode",
		Signature:   "base64.decode(encoded: string) -> bytes",
		Returns:     "bytes",
		Description: "Decodes a base64 string to bytes.",
		Example:     `base64.decode('aGVsbG8=')`,
		Category:    "encoding",
	},
}

// GetCELFunction returns a CEL function by name
func GetCELFunction(name string) *CELFunction {
	for _, fn := range CELFunctions {
		if fn.Name == name {
			return &fn
		}
	}
	return nil
}

// GetCELFunctionsByCategory returns all functions in a category
func GetCELFunctionsByCategory(category string) []CELFunction {
	var result []CELFunction
	for _, fn := range CELFunctions {
		if fn.Category == category {
			result = append(result, fn)
		}
	}
	return result
}

// GetCELFunctionsByPrefix returns all functions starting with the given prefix
func GetCELFunctionsByPrefix(prefix string) []CELFunction {
	var result []CELFunction
	for _, fn := range CELFunctions {
		if len(fn.Name) >= len(prefix) && fn.Name[:len(prefix)] == prefix {
			result = append(result, fn)
		}
	}
	return result
}

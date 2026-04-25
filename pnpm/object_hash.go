package pnpm

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// objectHashSHA256 computes the object-hash SHA-256/base64 checksum of a
// JSON value, matching the behavior of the npm object-hash library v3 with
// options: respectType=false, unorderedObjects=true, unorderedArrays=true.
//
// Returns the checksum prefixed with "sha256-" (pnpm v9+ format).
func objectHashSHA256(raw json.RawMessage) (string, error) {
	serialized, err := objectHashSerialize(raw)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(serialized))
	return "sha256-" + base64.StdEncoding.EncodeToString(sum[:]), nil
}

// objectHashMD5 computes the object-hash MD5/hex checksum of a JSON value.
// This matches pnpm v6-8 format (no prefix).
func objectHashMD5(raw json.RawMessage) (string, error) {
	serialized, err := objectHashSerialize(raw)
	if err != nil {
		return "", err
	}
	sum := md5.Sum([]byte(serialized))
	return hex.EncodeToString(sum[:]), nil
}

// objectHashSerialize produces the object-hash serialization of a JSON value.
// This replicates the npm object-hash library's internal format:
//   - objects: "object:N:key1:value1,key2:value2," (keys sorted, N = count)
//   - strings: "string:LEN:VALUE"
//   - numbers: "number:VALUE"
//   - booleans: "bool:VALUE"
//   - arrays: "array:N:value1,value2," (sorted for unorderedArrays)
//   - null: "Null"
func objectHashSerialize(raw json.RawMessage) (string, error) {
	var v interface{}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.UseNumber()
	if err := d.Decode(&v); err != nil {
		return "", fmt.Errorf("decoding JSON: %w", err)
	}
	var b strings.Builder
	serializeValue(&b, v)
	return b.String(), nil
}

func serializeValue(b *strings.Builder, v interface{}) {
	switch val := v.(type) {
	case map[string]interface{}:
		serializeObject(b, val)
	case []interface{}:
		serializeArray(b, val)
	case string:
		b.WriteString(fmt.Sprintf("string:%d:%s", len(val), val))
	case json.Number:
		b.WriteString("number:" + val.String())
	case bool:
		if val {
			b.WriteString("bool:true")
		} else {
			b.WriteString("bool:false")
		}
	case nil:
		b.WriteString("Null")
	default:
		b.WriteString(fmt.Sprintf("%v", val))
	}
}

func serializeObject(b *strings.Builder, obj map[string]interface{}) {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	b.WriteString(fmt.Sprintf("object:%d:", len(keys)))
	for _, k := range keys {
		serializeValue(b, k)
		b.WriteByte(':')
		serializeValue(b, obj[k])
		b.WriteByte(',')
	}
}

func serializeArray(b *strings.Builder, arr []interface{}) {
	// For unorderedArrays, sort elements by their serialized form.
	serialized := make([]string, len(arr))
	for i, elem := range arr {
		var sb strings.Builder
		serializeValue(&sb, elem)
		serialized[i] = sb.String()
	}
	sort.Strings(serialized)

	b.WriteString(fmt.Sprintf("array:%d:", len(arr)))
	for _, s := range serialized {
		b.WriteString(s)
		b.WriteByte(',')
	}
}

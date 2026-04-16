package crypto

import "testing"

// SHA-1 + Base64 vectors verified against .NET reference:
//
//	using System; using System.Text; using System.Security.Cryptography;
//	Convert.ToBase64String(SHA1.Create().ComputeHash(Encoding.UTF8.GetBytes(s)))
//
// (Also cross-checkable with `echo -n "..." | openssl sha1 -binary | base64`.)
var sha1b64Vectors = []struct {
	input    string
	expected string
}{
	{"", "2jmj7l5rSw0yVb/vlWAYkK/YBwk="},
	{"a", "hvfkN/qlp/zhXR3cuerq6jd2Z7g="},
	{"admin", "0DPiKuNIrrVmD8IUCuw1hQxNqZc="},
	{"password", "W6ph5Mm5Pz8GgiULbPgzG37mj9g="},
	{"123456", "fEqNCco3Yq9h5ZUglD3CZJT4lBs="},
	{"test", "qUqP5cyxm6YcTAhz05Hph5gvu9M="},
	{"aion", "MyjXuYCHO7TCZuKdEf4W5Sp2wNg="},
}

func TestSHA1Base64_ReferenceParity(t *testing.T) {
	for _, v := range sha1b64Vectors {
		got := SHA1Base64(v.input)
		if got != v.expected {
			t.Errorf("SHA1Base64(%q) = %s, want %s", v.input, got, v.expected)
		}
	}
}

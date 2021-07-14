/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package endpoint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEncrypt(t *testing.T) {
	// Verify that text encryption and decryption works
	aesKey := []byte("s%zF`.*'5`9.AhI2!B,.~hmbs^.*TL?;")
	plaintext := "Lorem Ipsum is simply dummy text of the printing and typesetting industry. Lorem Ipsum has been the industry's standard dummy text ever since the 1500s, when an unknown printer took a galley of type and scrambled it to make a type specimen book. It has survived not only five centuries, but also the leap into electronic typesetting, remaining essentially unchanged. It was popularised in the 1960s with the release of Letraset sheets containing Lorem Ipsum passages, and more recently with desktop publishing software like Aldus PageMaker including versions of Lorem Ipsum."
	encryptedtext, err := EncryptText(plaintext, aesKey, nil)
	require.NoError(t, err)
	decryptedtext, _, err := DecryptText(encryptedtext, aesKey)
	require.NoError(t, err)
	if plaintext != decryptedtext {
		t.Errorf("Original plain text %#v differs from the resulting decrypted text %#v", plaintext, decryptedtext)
	}

	// Verify that decrypt returns an error and empty data if wrong AES encryption key is used
	decryptedtext, _, err = DecryptText(encryptedtext, []byte("s'J!jD`].LC?g&Oa11AgTub,j48ts/96"))
	require.Error(t, err)
	if decryptedtext != "" {
		t.Error("Data decryption failed, empty string should be as result")
	}

	// Verify that decrypt returns an error and empty data if unencrypted input is is supplied
	decryptedtext, _, err = DecryptText(plaintext, aesKey)
	require.Error(t, err)
	if decryptedtext != "" {
		t.Errorf("Data decryption failed, empty string should be as result")
	}

	// Verify that a known encrypted text is decrypted to what is expected
	encryptedtext = "0Mfzf6wsN8llrfX0ucDZ6nlc2+QiQfKKedjPPLu5atb2I35L9nUZeJcCnuLVW7CVW3K0h94vSuBLdXnMrj8Vcm0M09shxaoF48IcCpD03XtQbKXqk2hPbsW6+JybvplHIQGr16/PcjUSObGmR9yjf38+qEltApkKvrPjsyw43BX4eE10rL0Bln33UJD7/w+zazRDPFlAcbGtkt0ETKHnvyB3/aCddLipvrhjCXj2ZY/ktRF6h716kJRgXU10dCIQHFYU45MIdxI+k10HK3yZqhI2V0Gp2xjrFV/LRQ7/OS9SFee4asPWUYxbCEsnOzp8qc0dCPFSo1dtADzWnUZnsAcbnjtudT4milfLJc5CxDk1v3ykqQ/ajejwHjWQ7b8U6AsTErbezfdcqrb5IzkLgHb5TosnfrdDmNc9GcKfpsrCHbVY8KgNwMVdtwavLv7d9WM6sooUlZ3t0sABGkzagXQmPRvwLnkSOlie5XrnzWo8/8/4UByLga29CaXO"
	decryptedtext, _, err = DecryptText(encryptedtext, aesKey)
	require.NoError(t, err)
	if decryptedtext != plaintext {
		t.Error("Decryption of text didn't result in expected plaintext result.")
	}
}

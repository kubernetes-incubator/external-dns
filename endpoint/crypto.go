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
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"

	log "github.com/sirupsen/logrus"
)

// EncryptText gzip input data and encrypts it using the supplied AES key
func EncryptText(text string, aesKey []byte, nonceEncoded []byte) (string, error) {
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if nonceEncoded == nil {
		if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
			return "", err
		}
	} else {
		if _, err = base64.StdEncoding.Decode(nonce, nonceEncoded); err != nil {
			return "", err
		}
	}

	data, err := compressData([]byte(text))
	if err != nil {
		return "", err
	}

	cipherData := gcm.Seal(nonce, nonce, data, nil)
	return base64.StdEncoding.EncodeToString(cipherData), nil
}

// DecryptText decrypt gziped data using a supplied AES encryption key ang ungzip it
// in case of decryption failed, will return original input and decryption error
func DecryptText(text string, aesKey []byte) (decryptResult string, encryptNonce string, err error) {
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return "", "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", err
	}
	nonceSize := gcm.NonceSize()
	data, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		return "", "", err
	}
	if len(data) <= nonceSize {
		return "", "", fmt.Errorf("the encoded data from text %#v is shorter than %#v bytes and can't be decoded", text, nonceSize)
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaindata, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", "", err
	}
	plaindata, err = decompressData(plaindata)
	if err != nil {
		log.Debugf("Failed to decompress data based on the base64 encoded text %#v. Got error %#v.", text, err)
		return "", "", err
	}

	return string(plaindata), base64.StdEncoding.EncodeToString(nonce), nil
}

// decompressData gzip compressed data
func decompressData(data []byte) (resData []byte, err error) {
	gz, err := gzip.NewReader(bytes.NewBuffer(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	var b bytes.Buffer
	if _, err = b.ReadFrom(gz); err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

// compressData by gzip, for minify data stored in registry
func compressData(data []byte) (compressedData []byte, err error) {
	var b bytes.Buffer
	gz, err := gzip.NewWriterLevel(&b, gzip.BestCompression)
	if err != nil {
		return nil, err
	}

	defer gz.Close()
	if _, err = gz.Write(data); err != nil {
		return nil, err
	}

	if err = gz.Flush(); err != nil {
		return nil, err
	}

	if err = gz.Close(); err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

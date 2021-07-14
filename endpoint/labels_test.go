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
	"fmt"
	"testing"

	"github.com/stretchr/testify/suite"
)

type LabelsSuite struct {
	suite.Suite
	aesKey                       []byte
	foo                          Labels
	fooAsText                    string
	fooAsTextWithQuotes          string
	fooAsTextEncrypted           string
	fooAsTextWithQuotesEncrypted string
	barText                      string
	barTextEncrypted             string
	barTextAsMap                 Labels
	noHeritageText               string
	wrongHeritageText            string
	multipleHeritageText         string //considered invalid
}

func (suite *LabelsSuite) SetupTest() {
	suite.foo = map[string]string{
		"owner":    "foo-owner",
		"resource": "foo-resource",
	}
	suite.aesKey = []byte(")K_Fy|?Z.64#UuHm`}[d!GC%WJM_fs{_")
	suite.fooAsText = "heritage=external-dns,external-dns/owner=foo-owner,external-dns/resource=foo-resource"
	suite.fooAsTextWithQuotes = fmt.Sprintf(`"%s"`, suite.fooAsText)
	suite.fooAsTextEncrypted = `+lvP8q9KHJ6BS6O81i2Q6DLNdf2JSKy8j/gbZKviTZlGYj7q+yDoYMgkQ1hPn6urtGllM5bfFMcaaHto52otQtiOYrX8990J3kQqg4s47m3bH3Ejl8RSxSSuWJM3HJtPghQzYg0/LSOsdQ0=`
	suite.fooAsTextWithQuotesEncrypted = fmt.Sprintf(`"%s"`, suite.fooAsTextEncrypted)
	suite.barTextAsMap = map[string]string{
		"owner":    "bar-owner",
		"resource": "bar-resource",
		"new-key":  "bar-new-key",
	}
	suite.barText = "heritage=external-dns,,external-dns/owner=bar-owner,external-dns/resource=bar-resource,external-dns/new-key=bar-new-key,random=stuff,no-equal-sign,," //also has some random gibberish
	suite.barTextEncrypted = "yi6vVATlgYN0enXBIupVK2atNUKtajofWMroWtvZjUanFZXlWvqjJPpjmMd91kv86bZj+syQEP0uR3TK6eFVV7oKFh/NxYyh238FjZ+25zlXW9TgbLoMalUNOkhKFdfXkLeeaqJjePB59t+kQBYX+ZEryK652asPs6M+xTIvtg07N7WWZ6SjJujm0RRISg=="
	suite.noHeritageText = "external-dns/owner=random-owner"
	suite.wrongHeritageText = "heritage=mate,external-dns/owner=random-owner"
	suite.multipleHeritageText = "heritage=mate,heritage=external-dns,external-dns/owner=random-owner"
}

func (suite *LabelsSuite) TestSerialize() {
	suite.Equal(suite.fooAsText, suite.foo.SerializePlain(false), "should serializeLabel")
	suite.Equal(suite.fooAsTextWithQuotes, suite.foo.SerializePlain(true), "should serializeLabel")
	suite.Equal(suite.fooAsText, suite.foo.Serialize(false, false, nil), "should serializeLabel")
	suite.Equal(suite.fooAsTextWithQuotes, suite.foo.Serialize(true, false, nil), "should serializeLabel")
	suite.Equal(suite.fooAsText, suite.foo.Serialize(false, false, suite.aesKey), "should serializeLabel")
	suite.Equal(suite.fooAsTextWithQuotes, suite.foo.Serialize(true, false, suite.aesKey), "should serializeLabel")
	suite.NotEqual(suite.fooAsText, suite.foo.Serialize(false, true, suite.aesKey), "should serializeLabel and encrypt")
	suite.NotEqual(suite.fooAsTextWithQuotes, suite.foo.Serialize(true, true, suite.aesKey), "should serializeLabel and encrypt")
}

func (suite *LabelsSuite) TestEncryptionNonceReUsage() {
	foo, err := NewLabelsFromString(suite.fooAsTextEncrypted, suite.aesKey)
	suite.NoError(err, "should succeed for valid label text")
	serialized := foo.Serialize(false, true, suite.aesKey)
	suite.Equal(serialized, suite.fooAsTextEncrypted, "serialized result should be equal")
}

func (suite *LabelsSuite) TestDeserialize() {
	foo, err := NewLabelsFromStringPlain(suite.fooAsText)
	suite.NoError(err, "should succeed for valid label text")
	suite.Equal(suite.foo, foo, "should reconstruct original label map")

	foo, err = NewLabelsFromStringPlain(suite.fooAsTextWithQuotes)
	suite.NoError(err, "should succeed for valid label text")
	suite.Equal(suite.foo, foo, "should reconstruct original label map")

	foo, err = NewLabelsFromString(suite.fooAsTextEncrypted, suite.aesKey)
	suite.NoError(err, "should succeed for valid encrypted label text")
	for key, val := range suite.foo {
		suite.Equal(val, foo[key], "should contains all keys from original label map")
	}

	foo, err = NewLabelsFromString(suite.fooAsTextWithQuotesEncrypted, suite.aesKey)
	suite.NoError(err, "should succeed for valid encrypted label text")
	for key, val := range suite.foo {
		suite.Equal(val, foo[key], "should contains all keys from original label map")
	}

	bar, err := NewLabelsFromStringPlain(suite.barText)
	suite.NoError(err, "should succeed for valid label text")
	suite.Equal(suite.barTextAsMap, bar, "should reconstruct original label map")

	bar, err = NewLabelsFromString(suite.barText, suite.aesKey)
	suite.NoError(err, "should succeed for valid encrypted label text")
	suite.Equal(suite.barTextAsMap, bar, "should reconstruct original label map")

	noHeritage, err := NewLabelsFromStringPlain(suite.noHeritageText)
	suite.Equal(ErrInvalidHeritage, err, "should fail if no heritage is found")
	suite.Nil(noHeritage, "should return nil")

	wrongHeritage, err := NewLabelsFromStringPlain(suite.wrongHeritageText)
	suite.Equal(ErrInvalidHeritage, err, "should fail if wrong heritage is found")
	suite.Nil(wrongHeritage, "if error should return nil")

	multipleHeritage, err := NewLabelsFromStringPlain(suite.multipleHeritageText)
	suite.Equal(ErrInvalidHeritage, err, "should fail if multiple heritage is found")
	suite.Nil(multipleHeritage, "if error should return nil")
}

func TestLabels(t *testing.T) {
	suite.Run(t, new(LabelsSuite))
}

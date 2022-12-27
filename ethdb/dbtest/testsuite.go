// Copyright (c) 2022 mobus sunsc0220@gmail.com
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package dbtest

import (
	"bytes"
	"testing"

	"github.com/ankr/dogesyncer/ethdb"
)

// TestDatabaseSuite runs a suite of tests against a KeyValueStore database
// implementation.
func TestDatabaseSuite(t *testing.T, New func() ethdb.Database) {
	t.Run("Iterator", func(t *testing.T) {
		tests := []struct {
			content map[string]string
			prefix  string
			start   string
			order   []string
		}{
			// Empty databases should be iterable
			{map[string]string{}, "", "", nil},
			{map[string]string{}, "non-existent-prefix", "", nil},

			// Single-item databases should be iterable
			{map[string]string{"key": "val"}, "", "", []string{"key"}},
			{map[string]string{"key": "val"}, "k", "", []string{"key"}},
			{map[string]string{"key": "val"}, "l", "", nil},

			// Multi-item databases should be fully iterable
			{
				map[string]string{"k1": "v1", "k5": "v5", "k2": "v2", "k4": "v4", "k3": "v3"},
				"", "",
				[]string{"k1", "k2", "k3", "k4", "k5"},
			},
			{
				map[string]string{"k1": "v1", "k5": "v5", "k2": "v2", "k4": "v4", "k3": "v3"},
				"k", "",
				[]string{"k1", "k2", "k3", "k4", "k5"},
			},
			{
				map[string]string{"k1": "v1", "k5": "v5", "k2": "v2", "k4": "v4", "k3": "v3"},
				"l", "",
				nil,
			},
			// Multi-item databases should be prefix-iterable
			{
				map[string]string{
					"ka1": "va1", "ka5": "va5", "ka2": "va2", "ka4": "va4", "ka3": "va3",
					"kb1": "vb1", "kb5": "vb5", "kb2": "vb2", "kb4": "vb4", "kb3": "vb3",
				},
				"ka", "",
				[]string{"ka1", "ka2", "ka3", "ka4", "ka5"},
			},
			{
				map[string]string{
					"ka1": "va1", "ka5": "va5", "ka2": "va2", "ka4": "va4", "ka3": "va3",
					"kb1": "vb1", "kb5": "vb5", "kb2": "vb2", "kb4": "vb4", "kb3": "vb3",
				},
				"kc", "",
				nil,
			},
			// Multi-item databases should be prefix-iterable with start position
			{
				map[string]string{
					"ka1": "va1", "ka5": "va5", "ka2": "va2", "ka4": "va4", "ka3": "va3",
					"kb1": "vb1", "kb5": "vb5", "kb2": "vb2", "kb4": "vb4", "kb3": "vb3",
				},
				"ka", "3",
				[]string{"ka3", "ka4", "ka5"},
			},
			{
				map[string]string{
					"ka1": "va1", "ka5": "va5", "ka2": "va2", "ka4": "va4", "ka3": "va3",
					"kb1": "vb1", "kb5": "vb5", "kb2": "vb2", "kb4": "vb4", "kb3": "vb3",
				},
				"ka", "8",
				nil,
			},
		}
		for i, tt := range tests {
			// Create the key-value data store
			db := New()
			for key, val := range tt.content {
				if err := db.Set(ethdb.BodyDBI, []byte(key), []byte(val)); err != nil {
					t.Fatalf("test %d: failed to insert item %s:%s into database: %v", i, key, val, err)
				}
			}
			// Iterate over the database with the given configs and verify the results
			for key, val := range tt.content {
				got, _, _ := db.Get(ethdb.BodyDBI, []byte(key))
				if !bytes.Equal(got, []byte(val)) {
					t.Errorf("test %d:  value mismatch: have %s, want %s", i, string(got), val)
				}
			}
			db.Close()
		}
	})

	t.Run("Batch", func(t *testing.T) {
		db := New()
		defer db.Close()

		b := db.Batch()
		for _, k := range []string{"1", "2", "3", "4"} {
			if err := b.Set(ethdb.BodyDBI, []byte(k), nil); err != nil {
				t.Fatal(err)
			}
		}

		if err := b.Write(); err != nil {
			t.Fatal(err)
		}

		if _, has, err := db.Get(ethdb.BodyDBI, []byte("1")); err != nil {
			t.Fatal(err)
		} else if has {
			t.Log("db contains element before batch write")
		}

	})

}

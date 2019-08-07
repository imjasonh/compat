/*
Copyright 2019 Google, Inc.

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

package convert

import (
	"reflect"
	"testing"

	gcb "google.golang.org/api/cloudbuild/v1"
)

func TestUnknownFields(t *testing.T) {
	b := &gcb.Build{
		Id: buildID,
		Steps: []*gcb.BuildStep{{
			Name: "gcr.io/$PROJECT_ID/my-builder",
			Args: []string{"gcr.io/$PROJECT_ID/thing:$REVISION_ID"},
			Env:  []string{"BUILD_ID=$BUILD_ID"},
		}},
		Images: []string{"gcr.io/foo/$BRANCH_NAME"},
		Options: &gcb.BuildOptions{
			Env: []string{"GLOBAL=${BUILD_ID}"},
		},
	}
	if err := SubstituteBuildFields(b); err != nil {
		t.Fatalf("Error while substituting build fields: %v", err)
	}
	if diff := jsondiff(b, &gcb.Build{
		Id: b.Id,
		Steps: []*gcb.BuildStep{{
			Name: "gcr.io/project-id/my-builder",
			Args: []string{"gcr.io/project-id/thing:"},
			Env:  []string{"BUILD_ID=build-id"},
		}},
		Images: []string{"gcr.io/foo/"},
		Options: &gcb.BuildOptions{
			Env: []string{"GLOBAL=build-id"},
		},
	}); diff != "" {
		t.Errorf("SubstituteBuildFields diff: %s", diff)
	}
}

// TestUserSubstitutions tests user-defined substitution.
func TestUserSubstitutions(t *testing.T) {
	for _, tc := range []struct {
		desc, template, want string
		substitutions        map[string]string
		wantErr              bool
	}{{
		desc:     "variable should be substituted",
		template: "Hello $_VAR",
		substitutions: map[string]string{
			"_VAR": "World",
		},
		want: "Hello World",
	}, {
		desc:     "only full variable should be substituted",
		template: "Hello $_VAR_FOO, $_VAR",
		substitutions: map[string]string{
			"_VAR": "World",
		},
		want: "Hello , World",
	}, {
		desc:     "variable should be substituted if sticked with a char respecting [^A-Z0-9_]",
		template: "Hello $_VARfoo",
		substitutions: map[string]string{
			"_VAR": "World",
		},
		want: "Hello Worldfoo",
	}, {
		desc:     "curly braced variable should be substituted",
		template: "Hello ${_VAR}_FOO",
		substitutions: map[string]string{
			"_VAR": "World",
		},
		want: "Hello World_FOO",
	}, {
		desc:     "variable should be substituted",
		template: "Hello, 世界  FOO$_VAR",
		substitutions: map[string]string{
			"_VAR": "World",
		},
		want: "Hello, 世界  FOOWorld",
	}, {
		desc:     "variable should be substituted, even if sticked",
		template: `Hello $_VAR$_VAR`,
		substitutions: map[string]string{
			"_VAR": "World",
		},
		want: `Hello WorldWorld`,
	}, {
		desc:     "variable should be substituted, even if preceded by $$",
		template: `$$$_VAR`,
		substitutions: map[string]string{
			"_VAR": "World",
		},
		want: `$World`,
	}, {
		desc:     "escaped variable should not be substituted",
		template: `Hello $${_VAR}_FOO, $_VAR`,
		substitutions: map[string]string{
			"_VAR": "World",
		},
		want: `Hello ${_VAR}_FOO, World`,
	}, {
		desc:     "escaped variable should not be substituted",
		template: `Hello $$$$_VAR $$$_VAR, $_VAR, $$_VAR`,
		substitutions: map[string]string{
			"_VAR": "World",
		},
		want: `Hello $$_VAR $World, World, $_VAR`,
	}, {
		desc:     "escaped variable should not be substituted",
		template: `$$_VAR`,
		substitutions: map[string]string{
			"_VAR": "World",
		},
		want: `$_VAR`,
	}, {
		desc:          "unmatched keys in the template for a built-in substitution will result in an empty string",
		template:      `Hello $BUILTIN_DEFINED_VARIABLE`,
		substitutions: map[string]string{},
		want:          "Hello ",
	}} {
		b := &gcb.Build{
			Id: buildID,
			Steps: []*gcb.BuildStep{{
				Dir: tc.template,
			}},
			Options: &gcb.BuildOptions{
				Env: []string{tc.template},
			},
			Substitutions: tc.substitutions,
			Tags:          []string{tc.template},
		}
		err := SubstituteBuildFields(b)
		switch {
		case tc.wantErr && err == nil:
			t.Errorf("%q: want error, got none.", tc.desc)
		case !tc.wantErr && err != nil:
			t.Errorf("%q: want no error, got %v.", tc.desc, err)
		}
		if err != nil {
			continue
		}
		if diff := jsondiff(b, &gcb.Build{
			Id: b.Id,
			Steps: []*gcb.BuildStep{{
				Dir: tc.want,
			}},
			Options: &gcb.BuildOptions{
				Env: []string{tc.want},
			},
			Tags:          []string{tc.want},
			Substitutions: b.Substitutions,
		}); diff != "" {
			t.Errorf("SubstituteBuildFields diff: %s", diff)
		}
	}
}

func TestFindTemplateParameters(t *testing.T) {
	input := `\$BAR$FOO$$BAZ$$$_BOO$$$$BAH`
	want := []*templateParameter{{
		Start: 1,
		End:   4,
		Key:   "BAR",
	}, {
		Start: 5,
		End:   8,
		Key:   "FOO",
	}, {
		Start:  9,
		End:    10,
		Escape: true,
	}, {
		Start:  14,
		End:    15,
		Escape: true,
	}, {
		Start: 16,
		End:   20,
		Key:   "_BOO",
	}, {
		Start:  21,
		End:    22,
		Escape: true,
	}, {
		Start:  23,
		End:    24,
		Escape: true,
	}}
	got := findTemplateParameters(input)
	if !reflect.DeepEqual(want, got) {
		t.Errorf("findTemplateParameters(%s): want %+v, got %+v", input, want, got)
	}
}

func TestFindValidKeyFromIndex(t *testing.T) {
	testCases := []struct {
		input string
		index int
		want  *templateParameter
	}{{
		input: `$BAR`,
		index: 0,
		want: &templateParameter{
			Start: 0,
			End:   3,
			Key:   "BAR",
		},
	}, {
		input: `$BAR $FOO`,
		index: 5,
		want: &templateParameter{
			Start: 5,
			End:   8,
			Key:   "FOO",
		},
	}, {
		input: `$_BAR$FOO`,
		index: 0,
		want: &templateParameter{
			Start: 0,
			End:   4,
			Key:   "_BAR",
		},
	}, {
		input: `${BAR}FOO`,
		index: 0,
		want: &templateParameter{
			Start: 0,
			End:   5,
			Key:   "BAR",
		},
	}, {
		input: `$BAR}FOO`,
		index: 0,
		want: &templateParameter{
			Start: 0,
			End:   3,
			Key:   "BAR",
		},
	}, {
		input: `${BARFOO`,
		index: 0,
		want:  nil,
	}}
	for _, tc := range testCases {
		got := findValidKeyFromIndex(tc.input, tc.index)
		if !reflect.DeepEqual(tc.want, got) {
			t.Errorf("findValidKeyFromIndex(%s, %d): want %+v, got %+v", tc.input, tc.index, tc.want, got)
		}
	}
}

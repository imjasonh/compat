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
	"fmt"
	"regexp"

	"github.com/GoogleCloudPlatform/compat/pkg/constants"
	gcb "google.golang.org/api/cloudbuild/v1"
)

// TODO: subst doesn't actually have to be pkg/convert, but it shares some
// testing utilities.

var (
	unbracedKeyRE   = `([A-Z_][A-Z0-9_]*)`
	bracedKeyRE     = fmt.Sprintf(`{%s}`, unbracedKeyRE)
	validSubstKeyRE = regexp.MustCompile(`^\$(?:` + bracedKeyRE + `|` + unbracedKeyRE + `)`)
)

// SubstituteBuildFields does an in-place string substitution of build parameters.
// User-defined substitutions are also accepted.
// If a built-in substitution value is not defined (perhaps a sourceless build,
// or storage source), then the corresponding substitutions will result in an
// empty string.
func SubstituteBuildFields(b *gcb.Build) error {
	builtInSubstitutions := map[string]string{
		"PROJECT_ID": constants.ProjectID,
		"BUILD_ID":   b.Id,
		// TODO: REPO_NAME, BRANCH_NAME, TAG_NAME, REVISION_ID, COMMIT_SHA, SHORT_SHA
	}

	replacements := map[string]string{}
	// Add built-in substitutions.
	for k, v := range builtInSubstitutions {
		replacements[k] = v
	}
	// Add user-defined substitutions, overriding built-in substitutions.
	for k, v := range b.Substitutions {
		replacements[k] = v
	}

	applyReplacements := func(in string) string {
		parameters := findTemplateParameters(in)
		var out []byte
		lastEnd := -1
		for _, p := range parameters {
			out = append(out, in[lastEnd+1:p.Start]...)
			val, ok := replacements[p.Key]
			if !ok {
				val = ""
			}
			// If Escape, `$$` has to be subtituted by `$`
			if p.Escape {
				val = "$"
			}
			out = append(out, []byte(val)...)
			lastEnd = p.End
		}
		out = append(out, in[lastEnd+1:]...)
		return string(out)
	}

	// Apply variable expansion to fields.
	if b.Options != nil {
		for i, e := range b.Options.Env {
			b.Options.Env[i] = applyReplacements(e)
		}

		b.Options.WorkerPool = applyReplacements(b.Options.WorkerPool)
	}
	for _, step := range b.Steps {
		step.Name = applyReplacements(step.Name)
		for i, a := range step.Args {
			step.Args[i] = applyReplacements(a)
		}
		for i, e := range step.Env {
			step.Env[i] = applyReplacements(e)
		}
		step.Dir = applyReplacements(step.Dir)
		step.Entrypoint = applyReplacements(step.Entrypoint)
	}
	for i, img := range b.Images {
		b.Images[i] = applyReplacements(img)
	}
	for i, t := range b.Tags {
		b.Tags[i] = applyReplacements(t)
	}
	b.LogsBucket = applyReplacements(b.LogsBucket)
	if b.Artifacts != nil {
		for i, img := range b.Artifacts.Images {
			b.Artifacts.Images[i] = applyReplacements(img)
		}
		if b.Artifacts.Objects != nil {
			b.Artifacts.Objects.Location = applyReplacements(b.Artifacts.Objects.Location)
			for i, p := range b.Artifacts.Objects.Paths {
				b.Artifacts.Objects.Paths[i] = applyReplacements(p)
			}
		}
	}

	return nil
}

// templateParameter represents the position of a Key in a string.
type templateParameter struct {
	Start, End int
	Key        string
	Escape     bool // if true, this parameter is `$$`
}

// findTemplateParameters finds all the parameters in the string `input`,
// which are not escaped, and returns an array.
func findTemplateParameters(input string) []*templateParameter {
	parameters := []*templateParameter{}
	i := 0
	for i < len(input) {
		// two consecutive $
		if input[i] == '$' && i < len(input)-1 && input[i+1] == '$' {
			p := &templateParameter{Start: i, End: i + 1, Escape: true}
			parameters = append(parameters, p)
			i += 2
			continue
		}
		// Unique $
		if input[i] == '$' {
			if p := findValidKeyFromIndex(input, i); p != nil {
				parameters = append(parameters, p)
				i = p.End // continue the search at the end of this parameter.
			}
		}
		i++
	}
	return parameters
}

// findValidKeyFromIndex finds the first valid key starting at index i.
func findValidKeyFromIndex(input string, i int) *templateParameter {
	p := &templateParameter{Start: i}
	indices := validSubstKeyRE.FindStringSubmatchIndex(input[i:])
	if len(indices) == 0 {
		return nil
	}
	exprIndices := indices[0:2]
	bracedIndices := indices[2:4]
	unbracedIndices := indices[4:6]
	// End of the expression.
	p.End = i + exprIndices[1] - 1
	// Find the not empty match.
	var keyIndices []int
	if bracedIndices[0] != -1 {
		keyIndices = bracedIndices
	} else {
		keyIndices = unbracedIndices
	}
	p.Key = string(input[i+keyIndices[0] : i+keyIndices[1]])
	return p
}

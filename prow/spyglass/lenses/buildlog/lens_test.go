/*
Copyright 2018 The Kubernetes Authors.

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

package buildlog

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	prowconfig "k8s.io/test-infra/prow/config"
	"k8s.io/test-infra/prow/spyglass/api"
	"k8s.io/test-infra/prow/spyglass/lenses/fake"
)

func TestExpand(t *testing.T) {
	cases := []struct {
		name string
		g    LineGroup
		want bool
	}{
		{
			name: "basic",
		},
		{
			name: "not enough",
			g: LineGroup{
				LogLines: make([]LogLine, moreLines-1),
			},
		},
		{
			name: "just enough",
			g: LineGroup{
				LogLines: make([]LogLine, moreLines),
			},
			want: true,
		},
		{
			name: "more than enough",
			g: LineGroup{
				LogLines: make([]LogLine, moreLines+1),
			},
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.g.Expand(); got != tc.want {
				t.Errorf("Expand() got %t, wanted %t", got, tc.want)
			}
		})
	}
}

func TestGroupLines(t *testing.T) {
	lorem := []string{
		"Lorem ipsum dolor sit amet",
		"consectetur adipiscing elit",
		"sed do eiusmod tempor incididunt ut labore et dolore magna aliqua",
		"Ut enim ad minim veniam",
		"quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat",
		"Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur",
		"Excepteur sint occaecat cupidatat non proident",
		"sunt in culpa qui officia deserunt mollit anim id est laborum",
	}
	tests := []struct {
		name   string
		lines  []string
		start  int
		end    int
		groups []LineGroup
	}{
		{
			name:   "Test empty log",
			lines:  []string{},
			groups: []LineGroup{},
		},
		{
			name:  "Test error highlighting",
			lines: []string{"This is an ErRoR message"},
			groups: []LineGroup{
				{
					Start:      0,
					End:        1,
					Skip:       false,
					ByteOffset: 0,
					ByteLength: 24,
				},
			},
		},
		{
			name:  "Test skip all",
			lines: lorem,
			groups: []LineGroup{
				{
					Start:      0,
					End:        8,
					Skip:       true,
					ByteOffset: 0,
					ByteLength: 437,
				},
			},
		},
		{
			name: "Test skip none",
			lines: []string{
				"a", "b", "c", "d", "e",
				"ERROR: Failed to immanentize the eschaton.",
				"a", "b", "c", "d", "e",
			},
			groups: []LineGroup{
				{
					Start:      0,
					End:        11,
					Skip:       false,
					ByteOffset: 0,
					ByteLength: 62,
				},
			},
		},
		{
			name: "Test skip threshold",
			lines: []string{
				"a", "b", "c", "d", // skip threshold unmet
				"a", "b", "c", "d", "e", "ERROR: Failed to immanentize the eschaton.", "a", "b", "c", "d", "e",
				"a", "b", "c", "d", "e", // skip threshold met
			},
			groups: []LineGroup{
				{
					Start:      0,
					End:        4,
					Skip:       false,
					ByteOffset: 0,
					ByteLength: 7,
				},
				{
					Start:      4,
					End:        15,
					Skip:       false,
					ByteOffset: 8,
					ByteLength: 62,
				},
				{
					Start:      15,
					End:        20,
					Skip:       true,
					ByteOffset: 71,
					ByteLength: 9,
				},
			},
		},
		{
			name: "Test nearby errors",
			lines: []string{
				"a", "b", "c",
				"don't panic",
				"a", "b", "c",
				"don't panic",
				"a", "b", "c",
			},
			groups: []LineGroup{
				{
					Start:      0,
					End:        11,
					Skip:       false,
					ByteOffset: 0,
					ByteLength: 41,
				},
			},
		},
		{
			name: "Test separated errors",
			lines: []string{
				"a", "b", "c",
				"don't panic",
				"a", "b", "c", "d", "e",
				"a", "b", "c",
				"a", "b", "c", "d", "e",
				"don't panic",
				"a", "b", "c",
			},
			groups: []LineGroup{
				{
					Start:      0,
					End:        9,
					Skip:       false,
					ByteOffset: 0,
					ByteLength: 27,
				},
				{
					Start:      9,
					End:        12,
					Skip:       false,
					ByteOffset: 28,
					ByteLength: 5,
				},
				{
					Start:      12,
					End:        21,
					Skip:       false,
					ByteOffset: 34,
					ByteLength: 27,
				},
			},
		},
	}
	art := "fake-artifact"
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := groupLines(&art, test.start, test.end, highlightLines(test.lines, 0, &art, defaultErrRE)...)
			if len(got) != len(test.groups) {
				t.Fatalf("Expected %d groups, got %d", len(test.groups), len(got))
			}
			for j, exp := range test.groups {
				if got[j].Start != exp.Start || got[j].End != exp.End {
					t.Fatalf("Group %d expected lines [%d, %d), got [%d, %d)", j, exp.Start, exp.End, got[j].Start, got[j].End)
				}
				if got[j].Skip != exp.Skip {
					t.Errorf("Lines [%d, %d) expected Skip = %t", exp.Start, exp.End, exp.Skip)
				}
				if got[j].ByteOffset != exp.ByteOffset {
					t.Errorf("Group %d expected ByteOffset %d, got %d.", j, exp.ByteOffset, got[j].ByteOffset)
				}
				if got[j].ByteLength != exp.ByteLength {
					t.Errorf("Group %d expected ByteLength %d, got %d.", j, exp.ByteLength, got[j].ByteLength)
				}
			}
		})
	}
}

func pstr(s string) *string { return &s }

func TestBody(t *testing.T) {
	const (
		anonLink   = "https://storage.googleapis.com/bucket/object/build-log.txt"
		cookieLink = "https://storage.cloud.google.com/bucket/object/build-log.txt"
	)
	render := func(views ...LogArtifactView) string {
		return executeTemplate(".", "body", buildLogsView{LogViews: views})
	}
	view := func(name, link string, groups []LineGroup) LogArtifactView {
		return LogArtifactView{
			ArtifactName: name,
			ArtifactLink: link,
			ViewAll:      true,
			LineGroups:   groups,
			ShowRawLog:   true,
		}
	}

	cases := []struct {
		name      string
		artifact  *fake.Artifact
		rawConfig json.RawMessage
		want      string
	}{
		{
			name: "empty",
			artifact: &fake.Artifact{
				Path:    "foo",
				Content: []byte(""),
			},
			want: render(view("foo", fake.NotFound, []LineGroup{
				{
					Start:        1,
					End:          1,
					ArtifactName: pstr("foo"),
					LogLines: []LogLine{
						{
							ArtifactName: pstr("foo"),
							Number:       1,
							SubLines: []SubLine{
								{},
							},
						},
					},
				},
			},
			)),
		},
		{
			name: "single",
			artifact: &fake.Artifact{
				Path:    "foo",
				Content: []byte("hello"),
			},
			want: render(view("foo", fake.NotFound, []LineGroup{
				{
					Start:        1,
					End:          1,
					ArtifactName: pstr("foo"),
					LogLines: []LogLine{
						{
							ArtifactName: pstr("foo"),
							Number:       1,
							SubLines: []SubLine{
								{
									Text: "hello",
								},
							},
						},
					},
				},
			})),
		},
		{
			name: "cookie savable",
			artifact: &fake.Artifact{
				Path:    "foo",
				Content: []byte("hello"),
				Link:    pstr(cookieLink),
			},
			want: render(func() LogArtifactView {
				lav := view("foo", cookieLink, []LineGroup{
					{
						Start:        1,
						End:          1,
						ArtifactName: pstr("foo"),
						LogLines: []LogLine{
							{
								ArtifactName: pstr("foo"),
								Number:       1,
								SubLines: []SubLine{
									{
										Text: "hello",
									},
								},
							},
						},
					},
				})
				lav.CanSave = true
				return lav
			}()),
		},
		{
			name: "savable",
			artifact: &fake.Artifact{
				Path:    "foo",
				Content: []byte("hello"),
				Link:    pstr(anonLink),
			},
			want: render(func() LogArtifactView {
				lav := view("foo", anonLink, []LineGroup{
					{
						Start:        1,
						End:          1,
						ArtifactName: pstr("foo"),
						LogLines: []LogLine{
							{
								ArtifactName: pstr("foo"),
								Number:       1,
								SubLines: []SubLine{
									{
										Text: "hello",
									},
								},
							},
						},
					},
				})
				lav.CanSave = true
				return lav
			}()),
		},
		{
			name: "focus",
			artifact: &fake.Artifact{
				Path: "foo",
				Content: func() []byte {
					var sb strings.Builder
					for i := 0; i < 100; i++ {
						sb.WriteString("word\n")
					}
					return []byte(sb.String())
				}(),
				Meta: map[string]string{
					focusStart: "20",
					focusEnd:   "35",
				},
			},
			want: render(view("foo", fake.NotFound, []LineGroup{
				{
					Start:        0,
					End:          14,
					ArtifactName: pstr("foo"),
					Skip:         true,
					ByteLength:   69,
					ByteOffset:   0,
					LogLines:     make([]LogLine, 15),
				},
				{
					Start:        15,
					End:          40,
					ArtifactName: pstr("foo"),
					LogLines: func() []LogLine {
						var out []LogLine
						const s = 20
						const e = 35
						for i := s - neighborLines; i <= e+neighborLines; i++ {
							out = append(out, LogLine{
								ArtifactName: pstr("foo"),
								Number:       i,
								Focused:      i >= s && i <= e,
								Clip:         i == s,
								SubLines: []SubLine{
									{
										Text: "word",
									},
								},
							})
						}
						return out
					}(),
				},
				{
					Start:        40,
					End:          101,
					ArtifactName: pstr("foo"),
					Skip:         true,
					ByteLength:   100*5 - 5*40,
					ByteOffset:   5 * 40,
					LogLines:     make([]LogLine, 101-40),
				},
			})),
		},
		{
			name: "missing artifact",
			want: render(),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var arts []api.Artifact
			if tc.artifact != nil {
				arts = []api.Artifact{tc.artifact}
			}
			const dir = ""
			const data = ""
			got := Lens{}.Body(arts, dir, data, tc.rawConfig, prowconfig.Spyglass{})
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Body() got unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCallback(t *testing.T) {
	render := func(groups []*LineGroup) string {
		return executeTemplate(".", "line groups", groups)
	}

	cases := []struct {
		name         string
		artifact     *fake.Artifact
		data         string
		rawConfig    json.RawMessage
		want         string
		wantArtifact func(fake.Artifact) fake.Artifact
	}{
		{
			name: "empty",
			data: `{"artifact": "foo"}`,
			artifact: &fake.Artifact{
				Path:    "foo",
				Content: []byte(""),
			},
			want: render([]*LineGroup{
				{
					Start:        1,
					End:          1,
					ArtifactName: pstr("foo"),
					LogLines: []LogLine{
						{
							ArtifactName: pstr("foo"),
							Number:       1,
							SubLines: []SubLine{
								{},
							},
						},
					},
				},
			}),
		},
		{
			name: "single",
			data: `{
				"artifact": "foo",
				"length": 5

			}`,
			artifact: &fake.Artifact{
				Path:    "foo",
				Content: []byte("hello"),
			},
			want: render([]*LineGroup{
				{
					Start:        1,
					End:          1,
					ArtifactName: pstr("foo"),
					LogLines: []LogLine{
						{
							ArtifactName: pstr("foo"),
							Number:       1,
							SubLines: []SubLine{
								{
									Text: "hello",
								},
							},
						},
					},
				},
			}),
		},
		{
			name: "multiple",
			data: `{
				"artifact": "foo",
				"length": 11

			}`,
			artifact: &fake.Artifact{
				Path:    "foo",
				Content: []byte("hello\nworld"),
			},
			want: render([]*LineGroup{
				{
					Start:        1,
					End:          2,
					ArtifactName: pstr("foo"),
					LogLines: []LogLine{
						{
							ArtifactName: pstr("foo"),
							Number:       1,
							SubLines: []SubLine{
								{
									Text: "hello",
								},
							},
						},
						{
							ArtifactName: pstr("foo"),
							Number:       2,
							SubLines: []SubLine{
								{
									Text: "world",
								},
							},
						},
					},
				},
			}),
		},
		{
			name: "top",
			data: `{
				"artifact": "foo",
				"top": 3,
				"length": 400
			}`,
			artifact: &fake.Artifact{
				Path: "foo",
				Content: func() []byte {
					var sb strings.Builder
					for i := 0; i < 100; i++ {
						sb.WriteString("word\n")
					}
					return []byte(sb.String())
				}(),
			},
			want: render([]*LineGroup{
				{
					Start:        1,
					End:          3,
					ArtifactName: pstr("foo"),
					LogLines: []LogLine{
						{
							ArtifactName: pstr("foo"),
							Number:       1,
							SubLines: []SubLine{
								{
									Text: "word",
								},
							},
						},
						{
							ArtifactName: pstr("foo"),
							Number:       2,
							SubLines: []SubLine{
								{
									Text: "word",
								},
							},
						},
						{
							ArtifactName: pstr("foo"),
							Number:       3,
							SubLines: []SubLine{
								{
									Text: "word",
								},
							},
						},
					},
				},
				{
					Start:        3,
					End:          81,
					ArtifactName: pstr("foo"),
					Skip:         true,
					ByteLength:   385,
					ByteOffset:   15,
					LogLines:     make([]LogLine, 77),
				},
			}),
		},
		{
			name: "bottom",
			data: `{
				"artifact": "foo",
				"bottom": 3,
				"length": 400
			}`,
			artifact: &fake.Artifact{
				Path: "foo",
				Content: func() []byte {
					var sb strings.Builder
					for i := 0; i < 100; i++ {
						sb.WriteString("word\n")
					}
					return []byte(sb.String())
				}(),
			},
			want: render([]*LineGroup{
				{
					Start:        0,
					End:          78,
					ArtifactName: pstr("foo"),
					Skip:         true,
					ByteLength:   389,
					ByteOffset:   0,
					LogLines:     make([]LogLine, 78),
				},
				{
					Start:        78,
					End:          80,
					ArtifactName: pstr("foo"),
					LogLines: []LogLine{
						{
							ArtifactName: pstr("foo"),
							Number:       79,
							SubLines: []SubLine{
								{
									Text: "word",
								},
							},
						},
						{
							ArtifactName: pstr("foo"),
							Number:       80,
							SubLines: []SubLine{
								{
									Text: "word",
								},
							},
						},
						{
							ArtifactName: pstr("foo"),
							Number:       81,
							SubLines: []SubLine{
								{
									Text: "",
								},
							},
						},
					},
				},
			}),
		},
		{
			name: "full",
			data: `{
				"artifact": "foo",
				"length": 400
			}`,
			artifact: &fake.Artifact{
				Path: "foo",
				Content: func() []byte {
					var sb strings.Builder
					for i := 0; i < 100; i++ {
						sb.WriteString("word\n")
					}
					return []byte(sb.String())
				}(),
			},
			want: render([]*LineGroup{
				{
					Start:        0,
					End:          81,
					ArtifactName: pstr("foo"),
					LogLines: func() []LogLine {
						out := make([]LogLine, 0, 81)
						for i := 0; i < 80; i++ {
							out = append(out, LogLine{
								ArtifactName: pstr("foo"),
								Number:       i + 1,
								SubLines: []SubLine{
									{
										Text: "word",
									},
								},
							})
						}
						out = append(out, LogLine{
							ArtifactName: pstr("foo"),
							Number:       81,
							SubLines: []SubLine{
								{
									Text: "",
								},
							},
						})
						return out
					}(),
				},
			}),
		},
		{
			name: "save",
			data: `{
				"artifact": "foo",
				"startLine": 7,
				"saveEnd": 20
			}`,
			artifact: &fake.Artifact{
				Path:    "foo",
				Content: []byte("irrelevant"),
			},
			want: "",
			wantArtifact: func(a fake.Artifact) fake.Artifact {
				a.Meta = map[string]string{
					focusStart: "7",
					focusEnd:   "20",
				}
				return a
			},
		},
		{
			name: "bad json",
			want: failedUnmarshal,
		},
		{
			name: "missing artifact",
			data: `{"artifact": "foo"}`,
			want: fmt.Sprintf(missingArtifact, "foo"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var arts []api.Artifact
			if tc.artifact != nil {
				arts = []api.Artifact{tc.artifact}
			}
			got := Lens{}.Callback(arts, "", tc.data, tc.rawConfig, prowconfig.Spyglass{})
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Callback() got unexpected diff (-want +got):\n%s", diff)
			}

			if tc.wantArtifact != nil {
				want := tc.wantArtifact(*tc.artifact)
				if diff := cmp.Diff(&want, tc.artifact); diff != "" {
					t.Errorf("Callback() got unexpected artifact diff (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func BenchmarkHighlightLines(b *testing.B) {
	lorem := []string{
		"Lorem ipsum dolor sit amet",
		"consectetur adipiscing elit",
		"sed do eiusmod tempor incididunt ut labore et dolore magna aliqua",
		"Ut enim ad minim veniam",
		"quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat",
		"Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur",
		"Excepteur sint occaecat cupidatat non proident",
		"sunt in culpa qui officia deserunt mollit anim id est laborum",
	}
	art := "fake-artifact"
	b.Run("HighlightLines", func(b *testing.B) {
		_ = highlightLines(lorem, 0, &art, defaultErrRE)
	})
}

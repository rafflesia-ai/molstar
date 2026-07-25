package job

import "testing"

func TestResolvedURLProviders(t *testing.T) {
	cases := []struct {
		name  string
		input Input
		want  string
	}{
		{"pdbe", Input{ID: "1CBS", Provider: "pdbe"}, "https://www.ebi.ac.uk/pdbe/entry-files/1cbs.bcif"},
		{"rcsb", Input{ID: "1cbs", Provider: "rcsb"}, "https://models.rcsb.org/1CBS.bcif"},
		{"default provider is pdbe", Input{ID: "1cbs"}, "https://www.ebi.ac.uk/pdbe/entry-files/1cbs.bcif"},
		{"alphafold bare uniprot uses current version", Input{ID: "P05067", Provider: "alphafold"}, "https://alphafold.ebi.ac.uk/files/AF-P05067-F1-model_v6.cif"},
		{"alphafold full id passes through", Input{ID: "AF-P05067-F1-model_v4", Provider: "alphafold"}, "https://alphafold.ebi.ac.uk/files/AF-P05067-F1-model_v4.cif"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.input.ResolvedURL()
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("ResolvedURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAlphaFoldModelVersionOverride(t *testing.T) {
	input := Input{ID: "P05067", Provider: "alphafold"}

	t.Setenv("MOLSTAR_ALPHAFOLD_MODEL_VERSION", "v5")
	got, err := input.ResolvedURL()
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://alphafold.ebi.ac.uk/files/AF-P05067-F1-model_v5.cif"; got != want {
		t.Fatalf("override ResolvedURL() = %q, want %q", got, want)
	}

	// A bare number is normalized to a v-prefixed version.
	t.Setenv("MOLSTAR_ALPHAFOLD_MODEL_VERSION", "7")
	got, err = input.ResolvedURL()
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://alphafold.ebi.ac.uk/files/AF-P05067-F1-model_v7.cif"; got != want {
		t.Fatalf("numeric override ResolvedURL() = %q, want %q", got, want)
	}
}

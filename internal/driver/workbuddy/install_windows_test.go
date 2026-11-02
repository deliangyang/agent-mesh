//go:build windows

package workbuddy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSniffWorkBuddyInstallDirsFindsRunningOrPATH(t *testing.T) {
	dirs := sniffWorkBuddyInstallDirs()
	if len(dirs) == 0 {
		t.Skip("no WorkBuddy install visible on this machine")
	}
	cli := filepath.Join(dirs[0], "resources", "app.asar.unpacked", "cli", "bin", "codebuddy")
	if _, err := os.Stat(cli); err != nil {
		t.Fatalf("install %q missing cli: %v", dirs[0], err)
	}
}

func TestUninstallExeDir(t *testing.T) {
	got := uninstallExeDir(`"H:\wetools\workbuddy\Uninstall WorkBuddy.exe" /allusers`)
	want := `H:\wetools\workbuddy`
	if filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSniffWorkBuddyFromPATH(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workbuddy")
	bin := filepath.Join(root, "resources", "app.asar.unpacked", "cli", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "WorkBuddy.exe"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WORKBUDDY_HOME", "")
	t.Setenv("WORKBUDDY_INSTALL_DIR", "")
	t.Setenv("WORKBUDDY_PATH", "")
	found := false
	for _, p := range sniffWorkBuddyFromPATH() {
		if filepath.Clean(p) == filepath.Clean(root) {
			found = true
		}
	}
	if !found {
		t.Fatalf("PATH sniff missed root; got %#v", sniffWorkBuddyFromPATH())
	}
}

func TestSniffWorkBuddyInstallDirsFromEnv(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workbuddy")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "WorkBuddy.exe"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKBUDDY_HOME", root)
	t.Setenv("WORKBUDDY_INSTALL_DIR", "")
	t.Setenv("WORKBUDDY_PATH", "")
	dirs := sniffWorkBuddyInstallDirs()
	if len(dirs) == 0 || filepath.Clean(dirs[0]) != filepath.Clean(root) {
		t.Fatalf("env sniff missed; got %#v", dirs)
	}
}

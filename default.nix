{ pkgs, self }:
pkgs.buildGoModule {
  pname = "beads";
  version = "0.38.0";

  src = self;

  # Point to the main Go package
  subPackages = [ "cmd/bd" ];
  doCheck = false;

  # Go module dependencies hash.
  # To update when go.mod/go.sum changes:
  #   1. Set vendorHash = pkgs.lib.fakeHash;
  #   2. Run: nix build 2>&1 | grep 'got:' | awk '{print $2}'
  #   3. Replace with the new hash
  vendorHash = "sha256-ovG0EWQFtifHF5leEQTFvTjGvc+yiAjpAaqaV0OklgE=";

  # Git is required for tests
  nativeBuildInputs = [ pkgs.git ];

  meta = with pkgs.lib; {
    description = "beads (bd) - Distributed, git-backed graph issue tracker for AI agents";
    homepage = "https://github.com/steveyegge/beads";
    license = licenses.mit;
    mainProgram = "bd";
    maintainers = [ ];
  };
}

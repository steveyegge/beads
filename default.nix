{ pkgs, self }:
pkgs.buildGoModule {
  pname = "beads";
  version = "0.37.0";

  src = self;

  # Point to the main Go package
  subPackages = [ "cmd/bd" ];
  doCheck = false;
  # Go module dependencies hash - if build fails with hash mismatch, update with the "got:" value
  # NOTE: Stale after BurntSushi/toml was marked as direct dependency (GH#749)
  # If Nix CI reports mismatch, extract the "got:" hash and update here
  vendorHash = "sha256-ovG0EWQFtifHF5leEQTFvTjGvc+yiAjpAaqaV0OklgE=";

  # Git is required for tests
  nativeBuildInputs = [ pkgs.git ];

  meta = with pkgs.lib; {
    description = "beads (bd) - An issue tracker designed for AI-supervised coding workflows";
    homepage = "https://github.com/steveyegge/beads";
    license = licenses.mit;
    mainProgram = "bd";
    maintainers = [ ];
  };
}

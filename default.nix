{ pkgs, self, buildGoModule ? pkgs.buildGoModule }:
buildGoModule {
  pname = "beads";
  version = "0.49.6";

  src = self;

  # Point to the main Go package
  subPackages = [ "cmd/bd" ];
  doCheck = false;
  # Go module dependencies hash - if build fails with hash mismatch, update with the "got:" value
  vendorHash = "sha256-dyu3rsuFOruhw7729LlCxM29PSPavscDxlTsWdSaZmQ=";

  # Git is required for tests
  nativeBuildInputs = [ pkgs.git ];
  # ICU headers/libs are required for go-icu-regex cgo build.
  buildInputs = [ pkgs.icu pkgs.icu.dev ];

  meta = with pkgs.lib; {
    description = "beads (bd) - An issue tracker designed for AI-supervised coding workflows";
    homepage = "https://github.com/steveyegge/beads";
    license = licenses.mit;
    mainProgram = "bd";
    maintainers = [ ];
  };
}

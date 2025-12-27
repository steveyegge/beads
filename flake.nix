{
  description = "beads (bd) - Distributed, git-backed graph issue tracker for AI agents";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.11";
  };

  outputs = { self, nixpkgs }:
    let
      # Systems to support
      supportedSystems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];

      # Helper to generate outputs for each system
      forAllSystems = nixpkgs.lib.genAttrs supportedSystems;

      # Get pkgs for a specific system
      pkgsFor = system: nixpkgs.legacyPackages.${system};
    in
    {
      packages = forAllSystems (system:
        let
          pkgs = pkgsFor system;
        in
        {
          default = pkgs.buildGoModule (finalAttrs: {
            pname = "beads";
            version = "0.38.0";

            src = ./.;

            subPackages = [ "cmd/bd" ];

            # Go module dependencies hash.
            # To update when go.mod/go.sum changes:
            #   1. Set vendorHash = pkgs.lib.fakeHash;
            #   2. Run: nix build 2>&1 | grep 'got:' | awk '{print $2}'
            #   3. Replace with the new hash
            vendorHash = "sha256-ovG0EWQFtifHF5leEQTFvTjGvc+yiAjpAaqaV0OklgE=";

            # Skip tests during nix build (they require git setup)
            doCheck = false;

            # Git is required for some build-time operations
            nativeBuildInputs = [ pkgs.git ];

            meta = with pkgs.lib; {
              description = "Distributed, git-backed graph issue tracker for AI agents";
              homepage = "https://github.com/steveyegge/beads";
              license = licenses.mit;
              mainProgram = "bd";
              maintainers = [ ];
              platforms = supportedSystems;
            };
          });
        }
      );

      apps = forAllSystems (system: {
        default = {
          type = "app";
          program = "${self.packages.${system}.default}/bin/bd";
        };
      });

      devShells = forAllSystems (system:
        let
          pkgs = pkgsFor system;
        in
        {
          default = pkgs.mkShell {
            buildInputs = with pkgs; [
              # Go toolchain (1.24+)
              go
              gopls
              gotools
              golangci-lint
              # Development utilities
              git
              sqlite
            ];

            shellHook = ''
              echo "beads development shell"
              echo "Go version: $(go version)"
            '';
          };
        }
      );
    };
}

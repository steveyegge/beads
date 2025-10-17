{
  description = "beads (bd) - An issue tracker designed for AI-supervised coding workflows";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachSystem [
      "x86_64-linux"
      "aarch64-linux"
      "x86_64-darwin"
      "aarch64-darwin"
    ] (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        version = "0.9.9";
      in
      {
        packages = {
          beads = pkgs.buildGoModule {
            pname = "beads";
            inherit version;

            src = self;

            # Point to the main Go package
            subPackages = [ "cmd/bd" ];

            # Go module dependencies hash (computed via nix build)
            # Run `nix-prefetch` or `nix build` with a fake hash to update
            vendorHash = "sha256-1ufUs1PvFGsSR0DTSymni3RqecEBzAm//OBUWgaTwEs=";

            # Set version via ldflags
            ldflags = [
              "-s"
              "-w"
              "-X main.Version=${version}"
              "-X main.Build=nix"
            ];

            meta = with pkgs.lib; {
              description = "beads (bd) - An issue tracker designed for AI-supervised coding workflows";
              longDescription = ''
                Beads is a lightweight memory system for coding agents, using a graph-based
                issue tracker. It provides dependency tracking, ready work detection, and
                git-versioned storage, making it perfect for AI-supervised workflows.
              '';
              homepage = "https://github.com/steveyegge/beads";
              license = licenses.mit;
              mainProgram = "bd";
              maintainers = [ ];
              platforms = platforms.unix;
            };
          };

          default = self.packages.${system}.beads;
        };

        apps.default = {
          type = "app";
          program = "${self.packages.${system}.default}/bin/bd";
        };

        # Development shell with Go and tools
        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            gopls
            gotools
            go-tools
            golangci-lint
            delve
          ];

          shellHook = ''
            echo "🔗 Beads development environment"
            echo "Go version: $(go version)"
            echo ""
            echo "Available commands:"
            echo "  go build ./cmd/bd        - Build the bd binary"
            echo "  go test ./...            - Run tests"
            echo "  golangci-lint run        - Run linters"
            echo "  nix build                - Build with Nix"
            echo ""
          '';
        };

        # NixOS module (optional, can be used in NixOS configurations)
        nixosModules.default = { config, lib, pkgs, ... }:
          with lib;
          let
            cfg = config.programs.beads;
          in {
            options.programs.beads = {
              enable = mkEnableOption "beads issue tracker";
              package = mkOption {
                type = types.package;
                default = self.packages.${system}.default;
                description = "The beads package to use";
              };
            };

            config = mkIf cfg.enable {
              environment.systemPackages = [ cfg.package ];
            };
          };
      }
    );
}

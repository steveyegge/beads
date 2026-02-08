{
  description = "beads (bd) - An issue tracker designed for AI-supervised coding workflows";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    flake-utils.lib.eachSystem
      [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ]
      (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          # Pin Go to 1.25.6 to satisfy go.mod minimum patch version.
          go_1_25_6 = pkgs.go_1_25.overrideAttrs (old: rec {
            version = "1.25.6";
            src = pkgs.fetchurl {
              url = "https://go.dev/dl/go${version}.src.tar.gz";
              hash = "sha256-WMv3ceRNdt5vVtGeM7d9dFoeSJNAkih15GWFuXXCsFk=";
            };
          });
          buildGoModule_1_25_6 = pkgs.buildGoModule.override { go = go_1_25_6; };
          bdBase = pkgs.callPackage ./default.nix {
            inherit pkgs self;
            buildGoModule = buildGoModule_1_25_6;
          };
          # Wrap the base package with shell completions baked in
          bd = pkgs.stdenv.mkDerivation {
            pname = "beads";
            version = bdBase.version;

            phases = [ "installPhase" ];

            installPhase = ''
              mkdir -p $out/bin
              cp ${bdBase}/bin/bd $out/bin/bd

              # Create 'beads' alias symlink
              ln -s bd $out/bin/beads

              # Generate shell completions
              mkdir -p $out/share/fish/vendor_completions.d
              mkdir -p $out/share/bash-completion/completions
              mkdir -p $out/share/zsh/site-functions

              $out/bin/bd completion fish > $out/share/fish/vendor_completions.d/bd.fish
              $out/bin/bd completion bash > $out/share/bash-completion/completions/bd
              $out/bin/bd completion zsh > $out/share/zsh/site-functions/_bd
            '';

            meta = bdBase.meta;
          };
        in
        {
          packages = {
            default = bd;

            # Keep separate completion packages for users who only want specific shells
            fish-completions = pkgs.runCommand "bd-fish-completions" { } ''
              mkdir -p $out/share/fish/vendor_completions.d
              ln -s ${bd}/share/fish/vendor_completions.d/bd.fish $out/share/fish/vendor_completions.d/bd.fish
            '';

            bash-completions = pkgs.runCommand "bd-bash-completions" { } ''
              mkdir -p $out/share/bash-completion/completions
              ln -s ${bd}/share/bash-completion/completions/bd $out/share/bash-completion/completions/bd
            '';

            zsh-completions = pkgs.runCommand "bd-zsh-completions" { } ''
              mkdir -p $out/share/zsh/site-functions
              ln -s ${bd}/share/zsh/site-functions/_bd $out/share/zsh/site-functions/_bd
            '';
          };

          apps.default = {
            type = "app";
            program = "${self.packages.${system}.default}/bin/bd";
          };

          devShells.default = pkgs.mkShell {
            buildInputs = with pkgs; [
              go_1_25_6
              git
              gopls
              gotools
              golangci-lint
              sqlite
            ];

            shellHook = ''
              echo "beads development shell"
              echo "Go version: $(go version)"
            '';
          };
        }
      );
}

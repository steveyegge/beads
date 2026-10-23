"""Hermetic Dolt CLI for Bazel tests.

The release pinned here must equal scripts/ci/install-dolt.sh (the CLI that
`go test` jobs put on PATH) and the dolt-sql-server image in
internal/testutil/testdoltcommon.go; scripts/ci_workflow_test.go enforces it.
Raise all three together, and only after the measurement described in
install-dolt.sh.

MODULE.bazel declares one repository per platform (dolt_<os>_<arch>); only the
one selected by //tools/bazel:dolt for the execution platform is fetched.
"""

DOLT_VERSION = "2.5.1"

# sha256 of the upstream release tarballs dolt-<os>-<arch>.tar.gz, keyed by
# version so that bumping DOLT_VERSION without adding its digests fails instead
# of reusing the previous release from the repository cache (which is keyed by
# sha256 alone). They match the asset digests GitHub reports for the release
# (`gh api repos/dolthub/dolt/releases/tags/v<version>`).
DOLT_SHA256 = {
    "2.5.1": {
        "darwin-amd64": "f77f5c596310f78369b6a1f0ef2147703857b3cf7220508082a8e7dde948fcc0",
        "darwin-arm64": "0c14f997f1bc72228d3b07c815610f7ccfa883d7b66add8245c10e75c540adaa",
        "linux-amd64": "970927001a0eebd07a51307e784d245bf2e95796a49709e8b8a474ac833bf9a3",
        "linux-arm64": "ea8ec8dc81a227d6543db6d832ed83c4bf9033899d0eafd10909292da05fcf8a",
    },
    # The migration corpus's external Dolt runtime (DOLT_TEST_RUNTIME_VERSION
    # and _SHA256 in scripts/migration-test/lib/versions.sh), linux/amd64
    # only: @dolt_test_runtime_linux_amd64 in MODULE.bazel.
    "2.1.8": {
        "linux-amd64": "f66318f08ed66e409fc39363ae0fff8ce6fbf6dba9f5bac632b91527b9632a74",
    },
    "2.2.0": {
        "darwin-amd64": "d40b57933e2a2c025a5a3c269eb87594b1aa71ead16c9902213d521529a19b02",
        "darwin-arm64": "c6737dc2c5806e2eeef4839ad76c28167c861f878af3071df1242a6589d81267",
        "linux-amd64": "1f7ad8c2622995789420a3fb0f2d16b4aa7430000a825dd91d5938f36480cbf6",
        "linux-arm64": "a49a566d7c1ee9fdff553644855737f9d87047557c1ce1951aa7e7053277f4af",
    },
}

_BUILD = """\
package(default_visibility = ["//visibility:public"])

exports_files(["dolt"])
"""

def _dolt_release_impl(rctx):
    platform = rctx.attr.platform
    version = rctx.attr.version or DOLT_VERSION
    digests = DOLT_SHA256.get(version)
    if digests == None:
        fail("DOLT_SHA256 has no digests for dolt {}".format(version))
    if platform not in digests:
        fail("no pinned dolt {} release for platform {}".format(version, platform))
    url = "https://github.com/dolthub/dolt/releases/download/v{v}/dolt-{p}.tar.gz".format(
        v = version,
        p = platform,
    )
    rctx.download_and_extract(
        url = url,
        sha256 = digests[platform],
        # Ties the repository-cache entry to this URL (and so this version).
        canonical_id = url,
        stripPrefix = "dolt-{}/bin".format(platform),
    )
    rctx.file("BUILD.bazel", _BUILD)

dolt_release = repository_rule(
    implementation = _dolt_release_impl,
    attrs = {
        "platform": attr.string(
            mandatory = True,
            doc = "Release platform suffix, for example linux-amd64.",
        ),
        "version": attr.string(
            doc = "Dolt release to fetch (a DOLT_SHA256 key); empty means DOLT_VERSION.",
        ),
    },
    doc = "Downloads a pinned Dolt CLI release for one platform and exports its `dolt` binary.",
)

def _hermetic_bin_impl(ctx):
    symlinks = {}
    for target, name in ctx.attr.tools.items():
        files = target[DefaultInfo].files.to_list()
        if len(files) != 1:
            fail("{} must provide exactly one file, got {}".format(target.label, len(files)))
        symlinks["{}/{}/{}".format(ctx.label.package, ctx.label.name, name)] = files[0]
    return [DefaultInfo(runfiles = ctx.runfiles(symlinks = symlinks))]

hermetic_bin = rule(
    implementation = _hermetic_bin_impl,
    attrs = {
        "tools": attr.label_keyed_string_dict(
            allow_files = True,
            mandatory = True,
            doc = "Executable file -> command name it is exposed under.",
        ),
    },
    doc = """Exposes tools as <package>/<name>/<command> in the runfiles of the
main repository: one stable directory a test wrapper can put first on PATH,
whatever canonical repository name the tool came from.""",
)

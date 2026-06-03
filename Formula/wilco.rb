class Wilco < Formula
  desc "Wilco build agent — runs iOS builds for your Wilco account on your Mac"
  homepage "https://withwilco.com"
  # Point this at a published release tarball before distributing the tap, e.g.:
  #   url "https://github.com/withwilco/wilco-agent/archive/refs/tags/v0.1.0.tar.gz"
  #   sha256 "<shasum -a 256 of that tarball>"
  url "https://github.com/withwilco/wilco-agent/archive/refs/tags/v0.1.0.tar.gz"
  sha256 "0000000000000000000000000000000000000000000000000000000000000000"
  license "MIT"
  version "0.1.0"

  depends_on "go" => :build
  depends_on :macos

  def install
    # Dependencies are vendored, so this builds offline with no module fetches.
    ldflags = "-s -w"
    system "go", "build", *std_go_args(ldflags: ldflags), "."
  end

  def caveats
    <<~EOS
      Get started by connecting this Mac to your Wilco account:

        wilco

      This checks Xcode + Fastlane, signs you in through your browser, and
      offers to keep the agent running in the background.

      To build iOS apps you also need Xcode (from the App Store) and Fastlane
      (run `wilco doctor` and it will offer to install Fastlane for you).
    EOS
  end

  test do
    assert_match "wilco #{version}", shell_output("#{bin}/wilco version")
  end
end

# TaskTrooper's Homebrew cask. This repository is its own tap:
#
#   brew tap makifbaysal/tasktrooper https://github.com/makifbaysal/tasktrooper
#   brew install --cask tasktrooper
#
# It resolves only once the repository is public: `brew` downloads the dmg from
# the release URL with no credentials. scripts/install.sh is the path that works
# while it is private, because it can use an authenticated `gh`.
#
# release.yml's cask job refreshes version and sha256 after every release tag;
# by hand: scripts/update-cask.sh <version> <path-to-dmg>
cask "tasktrooper" do
  version "0.2.4"
  sha256 "95ea4cb995e082c40a633e6b4648aa0fc92bf488b02e19289b6885bcdf19ead7"

  url "https://github.com/makifbaysal/tasktrooper/releases/download/v#{version}/TaskTrooper-#{version}-universal.dmg"
  name "TaskTrooper"
  desc "Local-first agent platform that runs coding agent CLIs on your own Mac"
  homepage "https://github.com/makifbaysal/tasktrooper"

  depends_on macos: :ventura

  app "TaskTrooper.app"

  # The app is ad-hoc signed and not notarized yet, and
  # Gatekeeper refuses a quarantined copy of that on first launch with a dialog
  # that offers no way past it.
  postflight do
    system_command "/usr/bin/xattr",
                   args: ["-dr", "com.apple.quarantine", "#{appdir}/TaskTrooper.app"]
  end

  zap trash: [
    "~/Library/Application Support/TaskTrooper",
    "~/Library/Logs/TaskTrooper",
    "~/Library/Preferences/ai.tasktrooper.desktop.plist",
  ]
end

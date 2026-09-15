# The cask, kept here and copied into the tap makifbaysal/homebrew-tasktrooper.
#
# It only resolves once the main repository is public: `brew` downloads the dmg
# from the release URL with no credentials, so while tasktrooper-oss is private
# every install fails with a 404. scripts/install.sh is the path that works in
# the meantime, because it can use an authenticated `gh`.
#
# Refresh it for a release with: scripts/update-cask.sh <version> <path-to-dmg>
cask "tasktrooper" do
  version "0.1.0"
  sha256 "898c52a171d4527428a3195e587654c09c4a0545651c3234310f58224933e54e"

  url "https://github.com/makifbaysal/tasktrooper-oss/releases/download/v#{version}/TaskTrooper-#{version}-universal.dmg"
  name "TaskTrooper"
  desc "Local-first agent platform that runs Claude Code on your own Mac"
  homepage "https://github.com/makifbaysal/tasktrooper-oss"

  depends_on macos: ">= :ventura"

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

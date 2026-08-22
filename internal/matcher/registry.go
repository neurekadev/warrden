// Package matcher contains the queue warning patterns shared by configuration and cleanup.
// Package matcher contains arr-specific queue warning patterns.
package matcher

// Valid reports whether key names a registered matcher.
func Valid(key string) bool {
	switch key {
	case "SAMPLE", "NO_FILES_ELIGIBLE", "NOT_CUSTOM_FORMAT_UPGRADE", "NO_AUDIO_TRACKS",
		"ARCHIVE_FILE", "INSUFFICIENT_FREE_SPACE", "FILE_UNPACKING", "UNABLE_TO_PARSE",
		"UNEXPECTED_ERROR", "LOCKED_FILE", "UNSUPPORTED_EXTENSION", "DOWNLOAD_CLIENT_ERROR",
		"IMPORT_PATH_INACCESSIBLE", "NOT_QUALITY_UPGRADE", "NOT_REVISION_UPGRADE", "DANGEROUS_FILE",
		"MATCHED_VIA_GRAB_HISTORY", "NOT_IN_GRABBED_RELEASE", "MOVIE_ALREADY_IMPORTED",
		"INVALID_SEASON_OR_EPISODE", "EPISODE_UNEXPECTED_FOLDER", "FULL_SEASON_PACK",
		"EPISODE_ALREADY_IMPORTED", "TITLE_MISSING", "TITLE_TBA", "EXISTING_FILE_MORE_EPISODES",
		"SPLIT_EPISODE", "MISSING_ABSOLUTE_NUMBER", "UNVERIFIED_SCENE_MAPPING", "ALBUM_ALREADY_IMPORTED",
		"FEWER_TRACKS", "UNMATCHED_TRACKS", "MISSING_TRACKS", "ALBUM_NOT_REQUESTED",
		"EXISTING_FILE_MORE_TRACKS", "DEST_FOLDER_NOT_ROOT", "ALBUM_MATCH_NOT_CLOSE",
		"NO_TRACKS_MATCHED", "TRACK_MATCH_NOT_CLOSE":
		return true
	default:
		return false
	}
}

// Patterns returns a new slice containing the patterns for a matcher and arr kind.
func Patterns(key, kind string) []string {
	switch key {
	case "SAMPLE":
		return forAll(kind, "Sample", "Unable to determine if file is a sample")
	case "NO_FILES_ELIGIBLE":
		return forAll(kind, "No files found are eligible")
	case "NOT_CUSTOM_FORMAT_UPGRADE":
		return forAll(kind, "Not a Custom Format upgrade")
	case "NO_AUDIO_TRACKS":
		return forAll(kind, "No audio tracks detected")
	case "ARCHIVE_FILE":
		return forAll(kind, "Found archive file")
	case "INSUFFICIENT_FREE_SPACE":
		return forAll(kind, "Not enough free space")
	case "FILE_UNPACKING":
		return forAll(kind, "File is still being unpacked")
	case "UNABLE_TO_PARSE":
		return forAll(kind, "Unable to parse file")
	case "UNEXPECTED_ERROR":
		return forAll(kind, "Unexpected error processing file")
	case "LOCKED_FILE":
		return forAll(kind, "Locked file, try again later")
	case "UNSUPPORTED_EXTENSION":
		return forAll(kind, "unsupported extension")
	case "DOWNLOAD_CLIENT_ERROR":
		return forAll(kind, "is reporting an error")
	case "IMPORT_PATH_INACCESSIBLE":
		return forAll(kind, "Import failed, path does not exist or is not accessible by")
	case "NOT_QUALITY_UPGRADE":
		switch kind {
		case "radarr":
			return []string{"Not an upgrade for existing movie"}
		case "sonarr":
			return []string{"Not an upgrade for existing episode"}
		case "lidarr":
			return []string{"Not an upgrade for existing track file", "Not an upgrade for existing album file"}
		case "whisparr":
			return []string{"Not an upgrade for existing episode", "Not an upgrade for existing movie"}
		}
	case "NOT_REVISION_UPGRADE":
		return forKinds(kind, []string{"radarr", "sonarr", "whisparr"}, "Not a quality revision upgrade")
	case "DANGEROUS_FILE":
		if kind == "lidarr" {
			return []string{"Found executable file"}
		}
		return forKinds(kind, []string{"radarr", "sonarr", "whisparr"}, "potentially dangerous file", "Found executable file")
	case "MATCHED_VIA_GRAB_HISTORY":
		switch kind {
		case "radarr":
			return []string{"Found matching movie via grab history"}
		case "sonarr":
			return []string{"Found matching series via grab history"}
		case "whisparr":
			return []string{"Found matching series via grab history", "Found matching movie via grab history"}
		}
	case "NOT_IN_GRABBED_RELEASE":
		if kind == "radarr" {
			return []string{"was not found in the grabbed release"}
		}
		return forKinds(kind, []string{"sonarr", "whisparr"}, "not found in the grabbed release")
	case "MOVIE_ALREADY_IMPORTED":
		return forKinds(kind, []string{"radarr", "whisparr"}, "Movie file already imported")
	case "INVALID_SEASON_OR_EPISODE":
		return forKinds(kind, []string{"sonarr", "whisparr"}, "Invalid season or episode")
	case "EPISODE_UNEXPECTED_FOLDER":
		return forKinds(kind, []string{"sonarr", "whisparr"}, "was unexpected considering the", "were unexpected considering the")
	case "FULL_SEASON_PACK":
		return forKinds(kind, []string{"sonarr", "whisparr"}, "all episodes in seasons")
	case "EPISODE_ALREADY_IMPORTED":
		return forKinds(kind, []string{"sonarr", "whisparr"}, "Episode file already imported")
	case "TITLE_MISSING":
		return forKinds(kind, []string{"sonarr", "whisparr"}, "does not have a title")
	case "TITLE_TBA":
		return forKinds(kind, []string{"sonarr", "whisparr"}, "has a TBA title")
	case "EXISTING_FILE_MORE_EPISODES":
		return forKinds(kind, []string{"sonarr", "whisparr"}, "contains more episodes than this file")
	case "SPLIT_EPISODE":
		return forKinds(kind, []string{"sonarr", "whisparr"}, "split into multiple files")
	case "MISSING_ABSOLUTE_NUMBER":
		return forKinds(kind, []string{"sonarr", "whisparr"}, "does not have an absolute episode number")
	case "UNVERIFIED_SCENE_MAPPING":
		return forKinds(kind, []string{"sonarr", "whisparr"}, "mapping for this episode has not been confirmed")
	case "ALBUM_ALREADY_IMPORTED":
		return forKinds(kind, []string{"lidarr"}, "Album already imported")
	case "FEWER_TRACKS":
		return forKinds(kind, []string{"lidarr"}, "Has fewer tracks than existing release")
	case "UNMATCHED_TRACKS":
		return forKinds(kind, []string{"lidarr"}, "Has unmatched tracks")
	case "MISSING_TRACKS":
		return forKinds(kind, []string{"lidarr"}, "Has missing tracks")
	case "ALBUM_NOT_REQUESTED":
		return forKinds(kind, []string{"lidarr"}, "Album release not requested")
	case "EXISTING_FILE_MORE_TRACKS":
		return forKinds(kind, []string{"lidarr"}, "contains more tracks than this file")
	case "DEST_FOLDER_NOT_ROOT":
		return forKinds(kind, []string{"lidarr"}, "Destination folder")
	case "ALBUM_MATCH_NOT_CLOSE":
		return forKinds(kind, []string{"lidarr"}, "Album match is not close enough")
	case "NO_TRACKS_MATCHED":
		return forKinds(kind, []string{"lidarr"}, "No tracks matched")
	case "TRACK_MATCH_NOT_CLOSE":
		return forKinds(kind, []string{"lidarr"}, "Track match is not close enough")
	}
	return nil
}

func forAll(kind string, patterns ...string) []string {
	return forKinds(kind, []string{"radarr", "sonarr", "lidarr", "whisparr"}, patterns...)
}

func forKinds(kind string, kinds []string, patterns ...string) []string {
	for _, candidate := range kinds {
		if candidate == kind {
			return append([]string(nil), patterns...)
		}
	}
	return nil
}

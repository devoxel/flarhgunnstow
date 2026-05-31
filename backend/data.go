package main

// audioExts enumerates the file extensions we treat as audio when scanning
// videoDir for local-file playlists. Lowercased; callers must lowercase the
// extension before lookup.
var audioExts = map[string]bool{
	".mp3":  true,
	".ogg":  true,
	".opus": true,
	".flac": true,
	".wav":  true,
	".m4a":  true,
	".aac":  true,
}

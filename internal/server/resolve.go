// Copyright 2023 defsub
//
// This file is part of Takeout.
//
// Takeout is free software: you can redistribute it and/or modify it under the
// terms of the GNU Affero General Public License as published by the Free
// Software Foundation, either version 3 of the License, or (at your option)
// any later version.
//
// Takeout is distributed in the hope that it will be useful, but WITHOUT ANY
// WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS
// FOR A PARTICULAR PURPOSE.  See the GNU Affero General Public License for
// more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with Takeout.  If not, see <https://www.gnu.org/licenses/>.

package server

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/takeoutfm/takeout/internal/config"
	"github.com/takeoutfm/takeout/internal/music"
	"github.com/takeoutfm/takeout/lib/date"
	"github.com/takeoutfm/takeout/lib/log"
	"github.com/takeoutfm/takeout/lib/pls"
	"github.com/takeoutfm/takeout/model"
	"github.com/takeoutfm/takeout/spiff"
	"github.com/takeoutfm/takeout/view"
)

// type Locator interface {
// 	LocateTrack(model.Track) string
// 	LocateMovie(model.Movie) string
// 	LocateEpisode(model.Episode) string

// 	FindArtist(string) (model.Artist, error)
// 	FindRelease(string) (model.Release, error)
// 	FindTrack(string) (model.Track, error)
// 	FindStation(string) (model.Station, error)
// 	FindMovie(string) (model.Movie, error)
// 	FindSeries(string) (model.Series, error)
// 	FindEpisode(string) (model.Episode, error)

// 	TrackImage(model.Track) string
// 	MovieImage(model.Movie) string
// 	EpisodeImage(model.Episode) string
// }

// type Context interface {
// 	view.Context
// 	Locator
// }

func trackEntry(ctx Context, t model.Track) spiff.Entry {
	return spiff.Entry{
		Creator:    t.PreferredArtist(),
		Album:      t.ReleaseTitle,
		Title:      t.Title,
		Image:      ctx.TrackImage(t),
		Location:   []string{ctx.LocateTrack(t)},
		Identifier: []string{t.ETag},
		Size:       []int64{t.Size},
		Date:       date.FormatJson(t.ReleaseDate),
	}
}

func movieEntry(ctx Context, m model.Movie) spiff.Entry {
	return spiff.Entry{
		Creator:    "Movie", // TODO need better creator
		Album:      m.Title,
		Title:      m.Title,
		Image:      ctx.MovieImage(m),
		Location:   []string{ctx.LocateMovie(m)},
		Identifier: []string{m.ETag},
		Size:       []int64{m.Size},
		Date:       date.FormatJson(m.Date),
	}
}

func episodeEntry(ctx Context, series model.Series, e model.Episode) spiff.Entry {
	author := e.Author
	if author == "" {
		author = series.Author
	}
	return spiff.Entry{
		Creator:    author,
		Album:      series.Title,
		Title:      e.Title,
		Image:      ctx.EpisodeImage(e),
		Location:   []string{ctx.LocateEpisode(e)},
		Identifier: []string{e.EID},
		Size:       []int64{e.Size},
		Date:       date.FormatJson(e.Date),
	}
}

func addTrackEntries(ctx Context, tracks []model.Track, entries []spiff.Entry) []spiff.Entry {
	for _, t := range tracks {
		entries = append(entries, trackEntry(ctx, t))
	}
	return entries
}

func addMovieEntries(ctx Context, movies []model.Movie, entries []spiff.Entry) []spiff.Entry {
	for _, m := range movies {
		entries = append(entries, movieEntry(ctx, m))
	}
	return entries
}

func addEpisodeEntries(ctx Context, series model.Series, episodes []model.Episode,
	entries []spiff.Entry) []spiff.Entry {
	for _, e := range episodes {
		entries = append(entries, episodeEntry(ctx, series, e))
	}
	return entries
}

// /music/artists/{id}/{res}
func resolveArtistRef(ctx Context, id, res string, entries []spiff.Entry) ([]spiff.Entry, error) {
	artist, err := ctx.FindArtist(id)
	if err != nil {
		return entries, err
	}
	v := ArtistView(ctx, artist)
	tracks := resolveArtistTrackList(v, res)
	entries = addTrackEntries(ctx, tracks.Tracks(), entries)
	return entries, nil
}

func resolveArtistTrackList(v *view.Artist, res string) view.TrackList {
	var tracks view.TrackList
	switch res {
	case "deep":
		tracks = v.Deep
	case "popular":
		tracks = v.Popular
	case "radio", "similar":
		tracks = v.Radio
	case "shuffle", "playlist":
		tracks = v.Shuffle
	case "singles":
		tracks = v.Singles
	case "tracks":
		tracks = v.Tracks
	}
	return tracks
}

// /music/releases/{id}/tracks
func resolveReleaseRef(ctx Context, id string, entries []spiff.Entry) ([]spiff.Entry, error) {
	release, err := ctx.FindRelease(id)
	if err != nil {
		return entries, err
	}
	rv := ReleaseView(ctx, release)
	tracks := rv.Tracks
	entries = addTrackEntries(ctx, tracks, entries)
	return entries, nil
}

// /music/tracks/{id}
func resolveTrackRef(ctx Context, id string, entries []spiff.Entry) ([]spiff.Entry, error) {
	t, err := ctx.FindTrack(id)
	if err != nil {
		return entries, err
	}
	entries = addTrackEntries(ctx, []model.Track{t}, entries)
	return entries, nil
}

// /movies/{id}
func resolveMovieRef(ctx Context, id string, entries []spiff.Entry) ([]spiff.Entry, error) {
	m, err := ctx.FindMovie(id)
	if err != nil {
		return entries, err
	}
	entries = addMovieEntries(ctx, []model.Movie{m}, entries)
	return entries, nil
}

// /podcasts/series/{id}
func resolveSeriesRef(ctx Context, id string, entries []spiff.Entry) ([]spiff.Entry, error) {
	series, err := ctx.FindSeries(id)
	if err != nil {
		return entries, err
	}
	pv := SeriesView(ctx, series)
	episodes := pv.Episodes
	if err != nil {
		return entries, err
	}
	entries = addEpisodeEntries(ctx, series, episodes, entries)
	return entries, nil
}

// /podcasts/episodes/{id}
func resolveEpisodeRef(ctx Context, id string, entries []spiff.Entry) ([]spiff.Entry, error) {
	episode, err := ctx.FindEpisode(id)
	if err != nil {
		return entries, err
	}
	series, err := ctx.FindSeries(episode.SID)
	if err != nil {
		return entries, err
	}
	episodes := []model.Episode{episode}
	entries = addEpisodeEntries(ctx, series, episodes, entries)
	return entries, nil
}

// /music/search?q={q}[&radio=1]
func resolveSearchRef(ctx Context, uri string, entries []spiff.Entry) ([]spiff.Entry, error) {
	u, err := url.Parse(uri)
	if err != nil {
		log.Println(err)
		return entries, err
	}

	q := u.Query().Get("q")
	radio := u.Query().Get("radio") != ""

	var tracks []model.Track
	if q != "" {
		limit := ctx.Config().Music.SearchLimit
		if radio {
			limit = ctx.Config().Music.RadioSearchLimit
		}
		tracks = ctx.Music().Search(q, limit)
	}

	if radio {
		tracks = music.Shuffle(tracks)
		limit := ctx.Config().Music.RadioLimit
		if len(tracks) > limit {
			tracks = tracks[:limit]
		}
	}

	entries = addTrackEntries(ctx, tracks, entries)
	return entries, nil
}

// /music/radio/{id}
func resolveRadioRef(ctx Context, id string, entries []spiff.Entry) ([]spiff.Entry, error) {
	s, err := ctx.FindStation(id)
	if err != nil {
		return entries, err
	}
	if !s.Visible(ctx.User().Name) {
		return entries, err
	}

	// rerun the station ref to get new tracks
	plist := RefreshStation(ctx, &s)

	entries = append(entries, plist.Spiff.Entries...)

	return entries, nil
}

// ref is a json encoded array of ContentDescription records. The result of
// this is intended to be a list of locations to the same stream encoded in
// different formats and allow the client to chose the best source.
//
// TODO - ideally the entries should include the source ContentType but
// currently there's no field for this in spiff. Clients can use the extension
// (aac, mp3, etc.) to determine ContentType for now.
func resolveSourceRef(ctx Context, ref string, s *model.Station, entries []spiff.Entry) ([]spiff.Entry, error) {
	var locations []string
	var sizes []int64
	var sources []config.ContentDescription
	json.Unmarshal([]byte(ref), &sources)

	queue := make(chan string, len(sources))
	for _, src := range sources {
		if strings.HasSuffix(src.URL, ".pls") {
			queue <- src.URL
		} else {
			locations = append(locations, src.URL)
			sizes = append(sizes, -1)
		}
	}

	count := len(queue)
	if count > 0 {
		// fetch many playlists concurrently and collect results and
		// errors
		results := make(chan pls.Playlist)
		errors := make(chan error)
		client := ctx.Config().NewGetter()
		for i := 0; i < count; i++ {
			go func(url string) {
				result, err := client.GetPLS(url)
				if err != nil {
					errors <- err
				} else {
					results <- result
				}
			}(<-queue)
		}
		for i := 0; i < count; i++ {
			select {
			case result := <-results:
				// TODO use the first pls entry for now
				locations = append(locations, result.Entries[0].File)
				sizes = append(sizes, int64(result.Entries[0].Length))
			case err := <-errors:
				fmt.Printf("src err %v\n", err)
			}
		}
	}

	entries = append(entries, spiff.Entry{
		Creator:    s.Creator,
		Album:      s.Name,
		Title:      s.Name,
		Image:      s.Image,
		Location:   locations,
		Size:       sizes,
		Identifier: []string{},
		Date:       date.FormatJson(time.Now()),
	})

	return entries, nil
}

func resolvePlsRef(ctx Context, url, creator, image string, entries []spiff.Entry) ([]spiff.Entry, error) {
	client := ctx.Config().NewGetter()
	result, err := client.GetPLS(url)
	if err != nil {
		return entries, err
	}

	for _, v := range result.Entries {
		entry := spiff.Entry{
			Creator:    creator,
			Album:      v.Title,
			Title:      v.Title,
			Image:      image,
			Location:   []string{v.File},
			Identifier: []string{},
			Size:       []int64{int64(v.Length)},
			Date:       date.FormatJson(time.Now()),
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// /activity/tracks
func resolveActivityTracksRef(ctx Context, entries []spiff.Entry) ([]spiff.Entry, error) {
	v := ActivityView(ctx)
	var tracks []model.Track
	// TODO only recent is supported for now
	for _, t := range v.RecentTracks {
		tracks = append(tracks, t.Track)
	}
	entries = addTrackEntries(ctx, tracks, entries)
	return entries, nil
}

// /activity/movies
func resolveActivityMoviesRef(ctx Context, entries []spiff.Entry) ([]spiff.Entry, error) {
	v := ActivityView(ctx)
	var movies []model.Movie
	// TODO only recent is supported for now
	for _, m := range v.RecentMovies {
		movies = append(movies, m.Movie)
	}
	entries = addMovieEntries(ctx, movies, entries)
	return entries, nil
}

func RefreshStation(ctx Context, s *model.Station) *spiff.Playlist {
	plist := spiff.NewPlaylist(spiff.TypeMusic)
	plist.Spiff.Location = fmt.Sprintf("/api/radio/stations/%d", s.ID)
	plist.Spiff.Title = s.Name
	plist.Spiff.Image = s.Image
	plist.Spiff.Creator = s.Creator
	plist.Spiff.Date = date.FormatJson(time.Now())

	if s.Type == music.TypeStream {
		// internet radio streams
		plist.Type = spiff.TypeStream
		if strings.HasSuffix(s.Ref, ".pls") {
			var entries []spiff.Entry
			entries, err := resolvePlsRef(ctx, s.Ref, s.Creator, s.Image, entries)
			if err != nil {
				log.Printf("pls error %s\n", err)
				return nil
			}
			plist.Spiff.Entries = entries
		} else if strings.HasPrefix(s.Ref, "[{") {
			var entries []spiff.Entry
			entries, err := resolveSourceRef(ctx, s.Ref, s, entries)
			if err != nil {
				log.Printf("src error %s\n", err)
				return nil
			}
			plist.Spiff.Entries = entries
		} else if strings.HasSuffix(s.Ref, ".mp3") ||
			strings.HasSuffix(s.Ref, ".aac") ||
			strings.HasSuffix(s.Ref, ".ogg") ||
			strings.HasSuffix(s.Ref, ".flac") {
			plist.Spiff.Entries = []spiff.Entry{{
				Creator:    s.Creator,
				Album:      "",
				Title:      s.Name,
				Image:      s.Image,
				Location:   []string{s.Ref},
				Identifier: []string{},
				Size:       []int64{-1},
				Date:       date.FormatJson(time.Now()),
			}}
		} else {
			// TODO add m3u, others?
			log.Printf("unsupported stream %s\n", s.Ref)
		}
	} else {
		plist.Spiff.Entries = []spiff.Entry{{Ref: s.Ref}}
		Resolve(ctx, plist)
		if plist.Spiff.Entries == nil {
			plist.Spiff.Entries = []spiff.Entry{}
		}
	}

	// TODO not saved for now
	//s.Playlist, _ = plist.Marshal()
	//m.UpdateStation(s)

	return plist
}

var (
	artistsRegexp      = regexp.MustCompile(`^/music/artists/([0-9a-zA-Z-]+)/([\w]+)$`)
	releasesRegexp     = regexp.MustCompile(`^/music/releases/([0-9a-zA-Z-]+)/tracks$`)
	tracksRegexp       = regexp.MustCompile(`^/music/tracks/([\d]+)$`)
	searchRegexp       = regexp.MustCompile(`^/music/search.*`)
	radioRegexp        = regexp.MustCompile(`^/music/radio/stations/([\d]+)$`)
	moviesRegexp       = regexp.MustCompile(`^/movies/([\d]+)$`)
	seriesRegexp       = regexp.MustCompile(`^/podcasts/series/([\d]+)$`)
	episodesRegexp     = regexp.MustCompile(`^/podcasts/episodes/([\d]+)$`)
	recentTracksRegexp = regexp.MustCompile(`^/activity/tracks$`)
	recentMoviesRegexp = regexp.MustCompile(`^/activity/movies$`)
)

func Resolve(ctx Context, plist *spiff.Playlist) (err error) {
	var entries []spiff.Entry

	for _, e := range plist.Spiff.Entries {
		if e.Ref == "" {
			entries = append(entries, e)
			continue
		}

		pathRef := e.Ref

		matches := artistsRegexp.FindStringSubmatch(pathRef)
		if matches != nil {
			entries, err = resolveArtistRef(ctx, matches[1], matches[2], entries)
			if err != nil {
				return err
			}
			continue
		}

		matches = releasesRegexp.FindStringSubmatch(pathRef)
		if matches != nil {
			entries, err = resolveReleaseRef(ctx, matches[1], entries)
			if err != nil {
				return err
			}
			continue
		}

		matches = tracksRegexp.FindStringSubmatch(pathRef)
		if matches != nil {
			entries, err = resolveTrackRef(ctx, matches[1], entries)
			if err != nil {
				return err
			}
			continue
		}

		if searchRegexp.MatchString(pathRef) {
			entries, err = resolveSearchRef(ctx, pathRef, entries)
			if err != nil {
				return err
			}
			continue
		}

		matches = radioRegexp.FindStringSubmatch(pathRef)
		if matches != nil {
			entries, err = resolveRadioRef(ctx, matches[1], entries)
			if err != nil {
				return err
			}
			continue
		}

		matches = moviesRegexp.FindStringSubmatch(pathRef)
		if matches != nil {
			entries, err = resolveMovieRef(ctx, matches[1], entries)
			if err != nil {
				return err
			}
			continue
		}

		matches = seriesRegexp.FindStringSubmatch(pathRef)
		if matches != nil {
			entries, err = resolveSeriesRef(ctx, matches[1], entries)
			if err != nil {
				return err
			}
			continue
		}

		matches = episodesRegexp.FindStringSubmatch(pathRef)
		if matches != nil {
			entries, err = resolveEpisodeRef(ctx, matches[1], entries)
			if err != nil {
				return err
			}
			continue
		}

		matches = recentTracksRegexp.FindStringSubmatch(pathRef)
		if matches != nil {
			entries, err = resolveActivityTracksRef(ctx, entries)
			if err != nil {
				return err
			}
			continue
		}

		matches = recentMoviesRegexp.FindStringSubmatch(pathRef)
		if matches != nil {
			entries, err = resolveActivityMoviesRef(ctx, entries)
			if err != nil {
				return err
			}
			continue
		}
	}

	plist.Spiff.Entries = entries

	return nil
}

func ResolveArtistPlaylist(ctx Context, v *view.Artist, path, nref string) *spiff.Playlist {
	// /music/artists/{id}/{resource}
	parts := strings.Split(nref, "/")
	res := parts[4]
	trackList := resolveArtistTrackList(v, res)

	plist := spiff.NewPlaylist(spiff.TypeMusic)
	plist.Spiff.Location = path
	plist.Spiff.Creator = v.Artist.Name
	plist.Spiff.Title = trackList.Title
	plist.Spiff.Image = v.Image
	plist.Spiff.Date = date.FormatJson(time.Now())
	if trackList.Tracks != nil {
		plist.Spiff.Entries = addTrackEntries(ctx, trackList.Tracks(), plist.Spiff.Entries)
	}
	return plist
}

func ResolveReleasePlaylist(ctx Context, v *view.Release, path string) *spiff.Playlist {
	// /music/release/{id}
	plist := spiff.NewPlaylist(spiff.TypeMusic)
	plist.Spiff.Location = path
	plist.Spiff.Creator = v.Release.Artist
	plist.Spiff.Title = v.Release.Name
	plist.Spiff.Image = v.Image
	plist.Spiff.Date = date.FormatJson(v.Release.Date)
	plist.Spiff.Entries = addTrackEntries(ctx, v.Tracks, plist.Spiff.Entries)
	return plist
}

func ResolveMoviePlaylist(ctx Context, v *view.Movie, path string) *spiff.Playlist {
	// /movies/{id}
	var directing []string
	for _, p := range v.Directing {
		directing = append(directing, p.Name)
	}
	plist := spiff.NewPlaylist(spiff.TypeVideo)
	plist.Spiff.Location = path
	plist.Spiff.Creator = strings.Join(directing, " \u2022 ")
	plist.Spiff.Title = v.Movie.Title
	plist.Spiff.Image = ctx.MovieImage(v.Movie)
	plist.Spiff.Date = date.FormatJson(v.Movie.Date)
	plist.Spiff.Entries = []spiff.Entry{
		movieEntry(ctx, v.Movie),
	}
	return plist
}

func ResolveSeriesPlaylist(ctx Context, v *view.Series, path string) *spiff.Playlist {
	// /podcasts/series/{id}
	plist := spiff.NewPlaylist(spiff.TypePodcast)
	plist.Spiff.Location = path
	plist.Spiff.Creator = v.Series.Author
	plist.Spiff.Title = v.Series.Title
	plist.Spiff.Image = v.Series.Image
	plist.Spiff.Date = date.FormatJson(v.Series.Date)
	plist.Spiff.Entries = addEpisodeEntries(ctx, v.Series, v.Episodes, plist.Spiff.Entries)
	return plist
}

func ResolveSeriesEpisodePlaylist(ctx Context, series *view.Series,
	v *view.Episode, path string) *spiff.Playlist {
	// /podcasts/episode/{id}
	plist := spiff.NewPlaylist(spiff.TypePodcast)
	plist.Spiff.Location = path
	plist.Spiff.Creator = series.Series.Author
	plist.Spiff.Title = v.Episode.Title
	plist.Spiff.Image = v.EpisodeImage(v.Episode)
	plist.Spiff.Date = date.FormatJson(v.Episode.Date)
	plist.Spiff.Entries = []spiff.Entry{
		episodeEntry(ctx, series.Series, v.Episode),
	}
	return plist
}

func ResolveActivityTracksPlaylist(ctx Context, v *view.ActivityTracks, res, path string) *spiff.Playlist {
	var tracks []model.Track
	artistMap := make(map[string]bool)
	for _, t := range v.Tracks {
		artistMap[t.Track.Artist] = true
		tracks = append(tracks, t.Track)
	}
	var artists []string
	for k := range artistMap {
		artists = append(artists, k)
	}
	sort.Slice(artists, func(i, j int) bool {
		return artists[i] < artists[j]
	})
	creators := strings.Join(artists, " \u2022 ")
	image := ""
	for _, t := range tracks {
		img := ctx.TrackImage(t)
		if img != "" {
			image = img
			break
		}
	}

	title := ""
	switch res {
	case "popular":
		title = ctx.Config().Activity.PopularTracksTitle
	case "recent":
		title = ctx.Config().Activity.RecentTracksTitle
	}

	plist := spiff.NewPlaylist(spiff.TypeMusic)
	plist.Spiff.Location = path
	plist.Spiff.Creator = creators
	plist.Spiff.Title = title
	plist.Spiff.Image = image
	plist.Spiff.Date = date.FormatJson(time.Now())
	plist.Spiff.Entries = addTrackEntries(ctx, tracks, plist.Spiff.Entries)
	return plist
}

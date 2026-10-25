# trainspot

Live NMBS train departures and journeys for any station in Belgium, right in your terminal.

```sh
go install github.com/PieterLuypaert/trainspot@latest
```

This needs Go 1.26 or newer. Run `trainspot -h` for every option.

```sh
trainspot                                 # plan a trip on a full screen
trainspot gent sint pieters               # the departure board of Gent-Sint-Pieters
trainspot gent sint pieters --to aarlen   # the next journeys from Gent to Arlon
trainspot mons                            # Bergen and Mons are the same station
trainspot liège-guillemins --lang fr      # station names in French: nl, fr, en or de
```

## The trip screen

```text
╭─ Gent-Sint-Pieters → Brussel-Zuid ──────────────────────── Sun 25 Oct 17:15 ─╮
│                                                                              │
│ ▸ 17:52 → 18:31      39 min   direct                                         │
│   18:03 → 18:50 +1   48 min   direct                                         │
│   18:13 → 19:07      54 min   direct                                         │
│   ────────────────────────────────────────────────────────────────────────   │
│     17:52  Gent-Sint-Pieters  10  IC 1517 to Genk                            │
│     18:31  Brussel-Zuid       12                                             │
│                                                                              │
│   +1 = 1 min late                                                            │
│   Live NMBS data via iRail, updated 17:15                                    │
╰─ ↑↓ choose · d departures · e edit · s swap · r refresh · q quit ────────────╯
```

Plain `trainspot` opens a full screen to plan a trip. Type where you leave from and
where you are going, and it suggests stations as you type, in any of the four languages.
Pick one with the arrow keys and enter, or tab to the other field. Once both are set,
enter shows the next journeys, with the stops of the one you choose below them.

On the journeys, `d` shows the departure board of where the trip starts, `s` swaps the
two stations, `e` edits them and `r` fetches everything again; it also refreshes by
itself every minute. trainspot remembers the trip in `~/.config/trainspot` and opens on
it the next time, so your daily trip is one word away.

## Departures

```text
$ trainspot namur
  Departures from Namen (Namur)                             Sun 25 Oct 15:15

  Time   Expected  To            Platform  Train     Crowd
  15:16  15:19 +3  Brussel-Zuid  9         IC 2136   low
  15:17  on time   Dinant        10        IC 2514
  15:28  on time   Liers         4         IC 3813   low

  +3 = 3 min late
  Live NMBS data via iRail, updated 15:15
```

For every departure the board shows when it is planned and when it really leaves:
on time, the new time with the minutes late, cancelled or departed. Next to that come
the destination, the platform (black on yellow when it changed), the train and how
busy NMBS expects it to be. Delays of five minutes or more are red. A legend explains
the marks on the board, and on a narrow terminal the least important columns make way.

## Journeys

```text
$ trainspot gent sint pieters --to aarlen
  Gent-Sint-Pieters → Aarlen                                Sun 25 Oct 16:36

  17:13 → 21:22   4h09   1 change
    17:13     Gent-Sint-Pieters  9   IC 3615 to Leuven
    18:07     Brussel-Zuid       14
              change 6 min
    18:13     Brussel-Zuid       15  IC 2118 to Aarlen
    21:22     Aarlen             2
```

With `--to`, trainspot lists the next journeys between two stations. Every word after
`--to` names the destination, so no quotes are needed. Each journey shows when it
leaves and arrives, how long it takes and how many changes it has, and below that
every stop with its platform and train and the time you have to change.

trainspot counts the delays into each change. When they leave too little time, it
says so: `change 2 min, tight` in yellow, or `change missed by 4 min` in red when the
next train leaves before you arrive. A walk between two stations is part of the change
after it, cancelled trains are marked, and a journey that leaves after today starts
with its day.

## Station names

Station names work in Dutch, French, English and German, with or without accents,
so `luik`, `liège` and `liege` all find the stations of Liège. When a name fits several
stations, such as `gent` or `brussel`, trainspot lists them with their local names
and asks which one you mean. Typos get suggestions.

## In scripts

Without a terminal, plain `trainspot` prints its help and exits with 2. Piped into
another command, trainspot prints plain text. In a terminal, `--no-color`
drops the colours and keeps only bold and dim text. Without a terminal to answer, a name that
fits several stations makes trainspot list them and exit with 2. Problems at iRail
exit with 1.

## Data

The departures and journeys come live from the [iRail API](https://docs.irail.be),
which serves the NMBS's own data. trainspot keeps the list of stations for a week in
`~/.cache/trainspot` and the last trip of the screen in `~/.config/trainspot`.

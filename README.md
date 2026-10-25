# trainspot

Live NMBS train departures and journeys for any station in Belgium, right in your terminal.

```sh
go install github.com/PieterLuypaert/trainspot@latest
```

This needs Go 1.25 or newer. Run `trainspot -h` for every option.

```sh
trainspot gent sint pieters               # the departure board of Gent-Sint-Pieters
trainspot gent sint pieters --to aarlen   # the next journeys from Gent to Arlon
trainspot mons                            # Bergen and Mons are the same station
trainspot liège-guillemins --lang fr      # station names in French: nl, fr, en or de
```

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

Piped into another command, trainspot prints plain text without colours, and
`--no-color` does the same in a terminal. Without a terminal to answer, a name that
fits several stations makes trainspot list them and exit with 2. Problems at iRail
exit with 1.

## Data

The departures and journeys come live from the [iRail API](https://docs.irail.be),
which serves the NMBS's own data. trainspot keeps the list of stations for a week in
`~/.cache/trainspot`.

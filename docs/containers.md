# Containers and logs

Both need the Docker socket mounted into Foyer (read-only is enough). Foyer
only reads from Docker, and samples resource stats only while a page showing
them is open.

## Containers

![The containers page](images/containers.png)

Every container on the host, grouped by compose project, with state and
health, CPU, memory (against its limit, or the host's memory) and network
traffic. Sort by name, CPU or memory, filter by name or image, and hide
stopped containers.

## Logs

![Following a container's logs](images/logs.png)

Click any container (or the logs button on a dashboard card) for its logs,
a replacement for Dozzle:

- **Follow** new lines live; the stream resumes where it left off if the
  connection drops.
- **Filter** lines, show **errors only**, toggle **timestamps** and
  **wrapping**.
- stderr lines are highlighted and ANSI colours are rendered.
- **Download** what's loaded, or **clear** the view.

The viewer shows the container's CPU, memory, network and process count
alongside. Logs open over whichever page you're on and have their own URL
(`#/containers?logs=jellyfin`), so they can be bookmarked.

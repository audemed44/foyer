# Drop

![Drop](images/drop.png)

**Drop** (the inbox in the top bar) is a shared inbox for notes, links and
files. Everything stays until you delete it, and every device sees the same
list.

- Type or paste into the box and press **Save** (or Ctrl+Enter).
- **Attach files**, paste an image, or drop files anywhere on the page.
- Text that's just a link, or a caption and a link (how most apps share a
  page), is saved as a link, and its page title is filled in.
- Notes and links can be copied; files downloaded or opened. Images show a
  thumbnail.

## From your phone

Install Foyer as an app (it needs HTTPS, see [installing](install.md#https-and-the-installable-app)).
On Android the installed app then appears in the share sheet: share a page,
some text, a photo or any file to **Foyer** and it lands in Drop.

## Sending files on to apps

Apps can take files from Drop. When an app's widget says it accepts a type
of file (see [the widget format](app-widgets.md#taking-files-from-drop)),
matching files get a button. Shelfloom takes EPUB and PDF files:

1. Share a book to Foyer from your phone.
2. Press **Add to library** on it in Drop.
3. Foyer uploads it to Shelfloom and shows "Added “Title” by Author" with an
   **Open** button that takes you to the book.

The file stays in Drop until you delete it.

## Storage and limits

Files are streamed to `/config/drop/files/` and listed in
`/config/drop/items.json`. Each file can be up to 512 MB
(`FOYER_DROP_MAX_MB`). Images, video, audio, PDFs and plain text open in the
browser; everything else downloads, so an uploaded web page can never run
as part of the dashboard.

As with the rest of Foyer there's no login: anyone who can reach it can
read and add to Drop.

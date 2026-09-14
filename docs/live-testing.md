# Testing a live booking

A passing local test or a healthy container does not prove that Yodel issued a
parking pass. A successful live test needs a completed job and the corresponding
Yodel confirmation or wallet pass matching the intended date, pass and vehicle.

1. Configure BlueBubbles or Twilio on **OTP sources** and select the default
   inbox. Open **Lakes → Buntzen Lake**, add the Yodel account, then choose
   **Sign in to Yodel** to verify the connection without reserving a pass.
2. Save the vehicle keyword and booking preferences on the lake page. If it has
   multiple enabled accounts, choose **Use for bookings** on the intended one;
   a sole enabled account is selected automatically. In **Settings**, keep
   **Booking confirmation → Manual approval** selected while testing and review
   the preparation and retry timing.
3. Open **Bookings**, choose **Book** for the lake, select a date and pass
   preference order, and press **Book**. The app opens the new job. It schedules
   preparation for an upcoming release or starts checking already released
   passes as soon as a worker is available. Already released passes always
   require approval, even if automatic confirmation is selected globally.
   Review the displayed date, pass, and vehicle before approving.
4. Check the completed job and Yodel's issued pass. If the outcome is uncertain,
   inspect Yodel's wallet or confirmation before retrying. The application keeps
   the profile/date reservation after confirmation starts to prevent duplicates.

The immediate path has a fixed 15-minute limit from enqueue, including queue time, OTP
retrieval and approval. Restarting the application does not extend it or retry
an interrupted action. Availability polling also respects the request's shorter
poll limit. Expired or cancelled attempts that never began confirmation may be
explicitly retried. The application does not clear a pre-existing Yodel cart;
inspect and clear it yourself before another attempt.

For a future release, **Book** captures the current preparation, authentication,
and release window together with the global manual or automatic confirmation
preference. A separate release-time test is needed to verify session warming and
release polling; an immediate checkout test does not exercise that timing.
Keep **Manual approval** selected while testing. Each press of **Book** creates
a job explicitly, including automatic confirmation for future releases when
selected in Settings. Use **Cancel job** on the job page to stop queued work.
Changing defaults does not change an existing job.

Check the current season dates and release rules in the
[BC Hydro reservation rules](https://www.bchydro.com/community/recreation_areas/buntzen_lake.html)
before choosing a date. Actual inventory can only be determined from Yodel.
Cancel an unused test pass through its reservation confirmation.

## Advanced checks for an existing request

The CLI retains sign-in checks and dry runs for an existing booking request ID.
These are operator diagnostics using a known request ID from the job record.
Replace `1` with that ID and run against the same appdata as the service:

```bash
docker compose exec lake-pass-bot lake-pass-bot auth-check --booking 1
docker compose exec lake-pass-bot lake-pass-bot dry-run --booking 1
```

A dry run checks login, the vehicle, and pass pages, then stops before adding a
pass to the cart. Neither command proves that a pass can be issued. These
advanced actions use the request's saved inputs; the web **Book** form creates a
new visit from current lake and account settings. See
[Common commands](../README.md#common-commands) for the CLI's booking modes.

The retired saved-request screens are no longer available. Existing job history
and reservation records are preserved. Removing the old UI does not release an
uncertain or confirmed account/date reservation or cancel an issued Yodel pass.

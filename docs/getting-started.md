# Getting started, step by step

This guide is for people who have never used a command line or a self-hosted tool. It takes about 15 minutes. If you get stuck, jump to [Troubleshooting](#troubleshooting).

Eraser runs on your own computer. It does not have an account system or a server of ours: your details and your email password never leave your machine except to talk to your own email provider.

## Before you start

You need:

- A computer running macOS, Windows or Linux. Eraser is not a phone app.
- An email address you are happy to send from (Gmail, iCloud, Fastmail and others work). Eraser sends each removal request from it, so brokers' replies land in your inbox.
- Your name and, if you want brokers to find you reliably, your postal address.

You do **not** need to know how to code.

## Step 1: Decide how emails get sent

Eraser can send the emails for you or hand them to you to send yourself. Pick one now, because it decides what you enter later.

| | Eraser sends them | You send them yourself |
|---|---|---|
| Setup | One extra step: create an app password (below) | Nothing to set up |
| Password given to Eraser | An app password, not your real one | None |
| Effort per broker | None, fully automatic | Open, review and send each email by hand |
| Good for | Most people | People who won't give any tool email access |

If you choose "send them yourself", skip ahead to [Step 5](#step-5-run-the-setup-wizard) and pick the **Skip** option on the email page. See [Sending by hand](#sending-by-hand) for how that works.

## Step 2: Get an app password (Gmail)

Many people ask: "Do I have to enter my Google password?" No. Eraser asks for an **app password**, a separate 16-character password Google generates for one app. It can send and read mail only, you can delete it at any time, and your real Google password stays private.

Eraser does not use "Sign in with Google" (OAuth). Google requires an app review for that, which a self-hosted, open-source tool cannot reasonably pass. An app password is the supported alternative.

1. Go to [myaccount.google.com/security](https://myaccount.google.com/security) and sign in.
2. Find **2-Step Verification**. If it says **Off**, turn it on and follow the prompts (a phone number or authenticator app is fine). App passwords are not available without it.
3. Open [myaccount.google.com/apppasswords](https://myaccount.google.com/apppasswords). If the page says the setting "isn't available for your account", see [Troubleshooting](#troubleshooting).
4. Type a name such as `Eraser` and click **Create**.
5. Google shows a 16-character password in a yellow box, like `abcd efgh ijkl mnop`. Copy it now. Google will not show it again. Spaces don't matter when you paste it into Eraser.

Keep that window open or paste the password somewhere temporary until Step 5.

Using another provider? The [provider table in the README](../README.md#setting-up-your-email-account) lists what each one needs (iCloud and Fastmail also use app passwords; Proton Mail needs its Bridge app).

## Step 3: Download Eraser

Pick the line that matches your computer.

**macOS or Linux with Homebrew** (open the Terminal app and paste these two lines):

```bash
brew tap drumandbytes/tap
brew install --cask drumandbytes/tap/eraser
```

**Windows, or macOS/Linux without Homebrew:**

1. Open the [Releases page](https://github.com/drumandbytes/eraser/releases/latest).
2. Under **Assets**, download the file for your system:
   - Windows: the file ending in `_windows_amd64.exe` (for example `eraser_2.0.1_windows_amd64.exe`)
   - Mac with Apple chip (M1 or newer): `_darwin_arm64.tar.gz`
   - Mac with Intel chip: `_darwin_amd64.tar.gz`
   - Linux: `_linux_amd64.tar.gz` (or `_arm64` on ARM)
3. Put it somewhere easy to find, such as your Downloads folder. On a Mac or Linux, double-click the `.tar.gz` to unpack it.

Not sure which Mac you have? Apple menu > **About This Mac**: "Chip: Apple M…" means Apple chip, "Processor: Intel" means Intel.

Windows may show a blue "Windows protected your PC" screen the first time because the file is not code-signed. Click **More info**, then **Run anyway**. On a Mac, if it says the app can't be opened, see [Troubleshooting](#troubleshooting).

## Step 4: Start Eraser

Eraser has no icon to click. You start it from a terminal window, once. Don't worry, it is one command.

**Windows**

1. Open the folder with `eraser…windows_amd64.exe`.
2. Click the folder path bar at the top of the window, type `cmd` and press Enter. A black window opens in that folder.
3. Type this and press Enter (replace the file name with yours, or press Tab to auto-complete it):

   ```text
   .\eraser_2.0.1_windows_amd64.exe serve
   ```

**macOS**

1. Open the **Terminal** app (press Cmd+Space, type `Terminal`, press Enter).
2. If you installed with Homebrew, type `eraser serve` and press Enter.
3. If you downloaded the file, type `cd ~/Downloads`, press Enter, then `./eraser serve` and press Enter.

**Linux**

Open a terminal in the folder with the file and run `./eraser serve`.

The window will print a few lines and stay busy. Leave it open: closing it stops Eraser.

Now open your browser and go to **<http://localhost:8080>**. This address only works on your own computer.

## Step 5: Run the setup wizard

The page walks you through three screens.

**1. Welcome.** Click **Get Started**.

**2. Your Information.** Enter your name, email address and, ideally, your address and country. Brokers match requests to records using these, so the more accurate they are, the more records get found. This stays on your computer. Optional fields (phone, date of birth, former names) help but are not required.

**3. Connect Your Email.**

- **Email provider:** choose **Gmail** (or yours). The server settings fill in automatically. Ignore the **Server settings** drop-down.
- **Email address:** your Gmail address.
- **App password:** paste the 16-character code from Step 2.
- Click **Continue**.

To send by hand instead, click **Skip — I'll send the emails myself** under the Continue button and leave the fields empty.

Eraser then offers to send a **test email** to yourself. Click it, check your inbox (and spam folder), and continue. If the test fails, see [Troubleshooting](#troubleshooting).

## Step 6: Send your first requests

1. Open **Brokers** in the top menu.
2. Click **Send to All**. Eraser sends up to 450 emails per day (just under Gmail's limit) and continues on the next run until every broker has been contacted.
3. Leave it be for a few days. The **Dashboard** shows who replied and what needs you.

Some brokers answer with a link to click or a form to fill in. Eraser lists these on the **Pipeline** page, so you can see what needs you. [Broker replies](broker-replies.md) explains what each reply type means.

Brokers re-list people over time, so repeat every few weeks. To have Eraser do that automatically, open **Settings > Automation** and install the schedule.

## Sending by hand

With the Skip option chosen, Eraser never sends anything. On the **Brokers** page each broker has an **Email** link showing the ready-to-send message with an **Open in mail app** button. Send it from your normal mail app, then press **Mark sent** so Eraser tracks it. It is slower for 700+ brokers, so many people use it for a handful of important ones and let Eraser send the rest.

## Troubleshooting

**"App passwords" page says it isn't available.** Google hides it when 2-Step Verification is off, for accounts in Advanced Protection, and for many work or school (Google Workspace) accounts where the administrator turned it off. Fix: turn on 2-Step Verification, or use a personal Gmail address, or use the Skip option and send by hand.

**The test email fails with "authentication failed" or "username and password not accepted".** The app password was typed wrongly or revoked. Create a new one in Step 2 and paste it again under **Settings**. Use the app password, not your Google password.

**The test email never arrives.** Check spam. Then check your Gmail **Sent** folder: if it is there, the send worked.

**macOS: "can't be opened because Apple cannot check it for malicious software".** The file is unsigned. Either right-click it, choose **Open**, then **Open** again, or run this once in Terminal from the folder holding the file:

```bash
xattr -dr com.apple.quarantine ./eraser
```

**`localhost:8080` doesn't load.** Make sure the Terminal/command window from Step 4 is still open and is still open. If another program uses port 8080, run `eraser serve -p 3000` and visit `http://localhost:3000`.

**I closed the window and Eraser stopped.** Run the start command from Step 4 again. Your settings and history are saved and come back.

**I'd rather not leave a window open.** Use **Settings > Automation** to have your computer run Eraser on a schedule in the background.

Still stuck? [Open an issue](https://github.com/drumandbytes/eraser/issues/new) and say which step failed and what the screen showed. Never paste your app password into an issue.

## What next

- [broker-replies.md](broker-replies.md): what to do with each kind of response
- [multi-profile.md](multi-profile.md): sending for a partner or relative as well
- [commands.md](commands.md): the full command and settings reference for when you want more control

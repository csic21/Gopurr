package io.github.gopurr.nativeapp;

import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.content.pm.PackageInstaller;
import android.os.Build;

/** Receives the system installer's result for an update session. */
public final class UpdateInstallReceiver extends BroadcastReceiver {
    static final String ACTION_STATUS = "io.github.gopurr.nativeapp.UPDATE_STATUS";
    static final String EXTRA_SESSION = "session";

    @Override public void onReceive(Context context, Intent intent) {
        if (!ACTION_STATUS.equals(intent.getAction())) return;
        int session = intent.getIntExtra(EXTRA_SESSION, -1);
        int status = intent.getIntExtra(PackageInstaller.EXTRA_STATUS, PackageInstaller.STATUS_FAILURE);
        String message = intent.getStringExtra(PackageInstaller.EXTRA_STATUS_MESSAGE);
        Intent action = null;
        if (status == PackageInstaller.STATUS_PENDING_USER_ACTION) {
            if (Build.VERSION.SDK_INT >= 33) action = intent.getParcelableExtra(Intent.EXTRA_INTENT, Intent.class);
            else action = intent.getParcelableExtra(Intent.EXTRA_INTENT);
        }
        UpdateManager.get(context).installResult(session, status, message, action);
    }
}

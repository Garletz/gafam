import android.content.Context;
import android.content.ContextWrapper;
import android.content.SharedPreferences;
import android.content.pm.ApplicationInfo;
import android.content.res.Resources;

public class FakeContext extends ContextWrapper {
    private ApplicationInfo mAppInfo;
    private Resources mRes = null;

    public FakeContext() {
        super(null);
        mAppInfo = new ApplicationInfo();
        mAppInfo.targetSdkVersion = 35;
        mAppInfo.packageName = "com.preappointment1.app";
    }

    @Override public ApplicationInfo getApplicationInfo() { return mAppInfo; }
    @Override public SharedPreferences getSharedPreferences(String n, int m) { return new InMemoryPrefs(); }
    @Override public Resources getResources() {
        if (mRes != null) return mRes;
        try { mRes = Resources.getSystem(); } catch (Exception e) {}
        return mRes;
    }
    @Override public Object getSystemService(String name) { return null; }
    @Override public String getPackageName() { return mAppInfo.packageName; }
    @Override public Context getApplicationContext() { return this; }
    @Override public java.io.File getFilesDir() { return new java.io.File("/tmp"); }
    @Override public java.io.File getCacheDir() { return new java.io.File("/tmp"); }
    @Override public java.io.File getDataDir() { return new java.io.File("/tmp"); }
    @Override public String getPackageResourcePath() { return "/tmp/p1.apk"; }
    @Override public String getPackageCodePath() { return "/tmp/p1.apk"; }
    @Override public String getOpPackageName() { return mAppInfo.packageName; }
    @Override public boolean isRestricted() { return false; }
    @Override public boolean isDeviceProtectedStorage() { return false; }
    @Override public boolean isUiContext() { return true; }
    @Override public android.os.Looper getMainLooper() { return android.os.Looper.getMainLooper(); }
    @Override public int checkCallingOrSelfPermission(String p) { return 0; }
    @Override public int checkSelfPermission(String p) { return 0; }
    @Override public int checkCallingPermission(String p) { return 0; }
    @Override public int checkPermission(String p, int pid, int uid) { return 0; }
    @Override public void enforceCallingOrSelfPermission(String p, String m) {}
    @Override public void enforcePermission(String p, int pid, int uid, String m) {}
    @Override public int getDisplayId() { return 0; }
    @Override public android.os.UserHandle getUser() { return null; }
    @Override public void startActivity(android.content.Intent i) {}
    @Override public void startActivity(android.content.Intent i, android.os.Bundle o) {}
    @Override public void sendBroadcast(android.content.Intent i) {}
    @Override public void sendBroadcast(android.content.Intent i, String p) {}
    @Override public android.content.ComponentName startService(android.content.Intent i) { return null; }
    @Override public boolean stopService(android.content.Intent i) { return false; }
    @Override public boolean bindService(android.content.Intent i, android.content.ServiceConnection c, int f) { return false; }
    @Override public void unbindService(android.content.ServiceConnection c) {}
    @Override public android.content.Intent registerReceiver(android.content.BroadcastReceiver r, android.content.IntentFilter f) { return null; }
    @Override public android.content.Intent registerReceiver(android.content.BroadcastReceiver r, android.content.IntentFilter f, int fl) { return null; }
    @Override public void unregisterReceiver(android.content.BroadcastReceiver r) {}
    @Override public android.content.res.Resources.Theme getTheme() { return null; }
    @Override public Context createConfigurationContext(android.content.res.Configuration c) { return this; }
    @Override public Context createDisplayContext(android.view.Display d) { return this; }
    @Override public android.content.pm.PackageManager getPackageManager() { return null; }
}

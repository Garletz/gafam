import urllib.request
import zipfile
import io

URL = "https://api.github.com/repos/Garletz/gafam/actions/runs/26834513665/logs"
print(f"Fetching {URL}...")

req = urllib.request.Request(URL)
try:
    with urllib.request.urlopen(req) as response:
        zip_data = response.read()
        
    with zipfile.ZipFile(io.BytesIO(zip_data)) as z:
        for filename in z.namelist():
            if "Run Docker Image" in filename:
                print(f"\n--- {filename} ---")
                print(z.read(filename).decode('utf-8'))
except Exception as e:
    print("Error:", e)

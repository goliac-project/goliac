import{_ as s,c as a,o as t,ae as e}from"./chunks/framework.C0FZg82U.js";const c=JSON.parse('{"title":"Quick start for existing Github organization","description":"","frontmatter":{},"headers":[],"relativePath":"quick_start.md","filePath":"quick_start.md"}'),l={name:"quick_start.md"};function n(h,i,p,k,r,o){return t(),a("div",null,i[0]||(i[0]=[e(`<h1 id="quick-start-for-existing-github-organization" tabindex="-1">Quick start for existing Github organization <a class="header-anchor" href="#quick-start-for-existing-github-organization" aria-label="Permalink to &quot;Quick start for existing Github organization&quot;">​</a></h1><p>For the full installation documentation, check the <a href="./installation.html">installation</a> page</p><h2 id="preparation" tabindex="-1">Preparation <a class="header-anchor" href="#preparation" aria-label="Permalink to &quot;Preparation&quot;">​</a></h2><h3 id="you-need-a-github-app" tabindex="-1">You need a Github app <a class="header-anchor" href="#you-need-a-github-app" aria-label="Permalink to &quot;You need a Github app&quot;">​</a></h3><p>As a Github org admin, in GitHub:</p><ul><li>Register new GitHub App <ul><li>in your profile settings, go to <code>Developer settings</code>/<code>GitHub Apps</code></li><li>Click on <code>New GitHub App</code></li></ul></li><li>Give basic information: <ul><li>GitHub App name can be <code>&lt;yourorg&gt;-goliac-app</code> (it will be used in the rulesets later)</li><li>Homepage URL can be <code>https://github.com/Alayacare/goliac</code></li><li>Disable the active Webhook</li></ul></li><li>Under Organization permissions <ul><li>Give Read/Write access to <code>Administration</code></li><li>Give Read/Write access to <code>Members</code></li></ul></li><li>Under Repository permissions <ul><li>Give Read/Write access to <code>Administration</code></li><li>Give Read/Write access to <code>Content</code></li></ul></li><li>Where can this GitHub App be installed: <code>Only on this account</code></li><li>And Create</li><li>then you must <ul><li>collect the AppID</li><li>Generate (and collect) a private key (file)</li></ul></li><li>Go to the left tab &quot;Install App&quot; <ul><li>Click on &quot;Install&quot;</li></ul></li></ul><h3 id="get-the-goliac-binary" tabindex="-1">Get the Goliac binary <a class="header-anchor" href="#get-the-goliac-binary" aria-label="Permalink to &quot;Get the Goliac binary&quot;">​</a></h3><div class="language-shell vp-adaptive-theme"><button title="Copy Code" class="copy"></button><span class="lang">shell</span><pre class="shiki shiki-themes github-light github-dark vp-code" tabindex="0"><code><span class="line"><span style="--shiki-light:#6F42C1;--shiki-dark:#B392F0;">curl</span><span style="--shiki-light:#005CC5;--shiki-dark:#79B8FF;"> -o</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> goliac</span><span style="--shiki-light:#005CC5;--shiki-dark:#79B8FF;"> -L</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> https://github.com/Alayacare/goliac/releases/latest/download/goliac-\`</span><span style="--shiki-light:#6F42C1;--shiki-dark:#B392F0;">uname</span><span style="--shiki-light:#005CC5;--shiki-dark:#79B8FF;"> -s</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;">\`</span><span style="--shiki-light:#6F42C1;--shiki-dark:#B392F0;">-</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;">\`</span><span style="--shiki-light:#6F42C1;--shiki-dark:#B392F0;">uname</span><span style="--shiki-light:#005CC5;--shiki-dark:#79B8FF;"> -m</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;">\`</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;"> &amp;&amp; </span><span style="--shiki-light:#6F42C1;--shiki-dark:#B392F0;">chmod</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> +x</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> goliac</span></span></code></pre></div><h3 id="create-a-goliac-admin-team" tabindex="-1">Create a goliac admin team <a class="header-anchor" href="#create-a-goliac-admin-team" aria-label="Permalink to &quot;Create a goliac admin team&quot;">​</a></h3><p>If you dont have yet one, you will need to create a team in Github, where you will add your IT/Github admins (in our example, the team is called <code>goliac-admin</code> ).</p><h2 id="scaffold-and-test" tabindex="-1">Scaffold and test <a class="header-anchor" href="#scaffold-and-test" aria-label="Permalink to &quot;Scaffold and test&quot;">​</a></h2><h3 id="scaffold" tabindex="-1">Scaffold <a class="header-anchor" href="#scaffold" aria-label="Permalink to &quot;Scaffold&quot;">​</a></h3><p>And now you can use the goliac application to assist you:</p><div class="language-shell vp-adaptive-theme"><button title="Copy Code" class="copy"></button><span class="lang">shell</span><pre class="shiki shiki-themes github-light github-dark vp-code" tabindex="0"><code><span class="line"><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">export</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;"> GOLIAC_GITHUB_APP_ID</span><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">=&lt;</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;">appid</span><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">&gt;</span></span>
<span class="line"><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">export</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;"> GOLIAC_GITHUB_APP_PRIVATE_KEY_FILE</span><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">=&lt;</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;">private key filename</span><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">&gt;</span></span>
<span class="line"><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">export</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;"> GOLIAC_GITHUB_APP_ORGANIZATION</span><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">=&lt;</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;">your github organization</span><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">&gt;</span></span>
<span class="line"><span style="--shiki-light:#6F42C1;--shiki-dark:#B392F0;">./goliac</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> scaffold</span><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;"> &lt;</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;">director</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;">y</span><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">&gt;</span><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;"> &lt;</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;">goliac-admin</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> team</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> you</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> want</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> to</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> creat</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;">e</span><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">&gt;</span></span></code></pre></div><p>So something like</p><div class="language-shell vp-adaptive-theme"><button title="Copy Code" class="copy"></button><span class="lang">shell</span><pre class="shiki shiki-themes github-light github-dark vp-code" tabindex="0"><code><span class="line"><span style="--shiki-light:#6F42C1;--shiki-dark:#B392F0;">mkdir</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> goliac-teams</span></span>
<span class="line"><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">export</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;"> GOLIAC_GITHUB_APP_ID</span><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">=</span><span style="--shiki-light:#005CC5;--shiki-dark:#79B8FF;">355525</span></span>
<span class="line"><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">export</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;"> GOLIAC_GITHUB_APP_PRIVATE_KEY_FILE</span><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">=</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;">goliac-project-app.2023-07-03.private-key.pem</span></span>
<span class="line"><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">export</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;"> GOLIAC_GITHUB_APP_ORGANIZATION</span><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">=</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;">goliac-project</span></span>
<span class="line"><span style="--shiki-light:#6F42C1;--shiki-dark:#B392F0;">./goliac</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> scaffold</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> goliac-teams</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> goliac-admin</span></span></code></pre></div><p>The application will connect to your GitHub organization and will try to guess</p><ul><li>your users</li><li>your teams</li><li>the repos associated with your teams</li></ul><p>And it will create the corresponding structure into the Goliac &quot;teams&quot; directory.</p><h3 id="clean-up-to-start" tabindex="-1">Clean up to start <a class="header-anchor" href="#clean-up-to-start" aria-label="Permalink to &quot;Clean up to start&quot;">​</a></h3><p>If you want, you can remove (for now) part or all repositories:</p><div class="language-shell vp-adaptive-theme"><button title="Copy Code" class="copy"></button><span class="lang">shell</span><pre class="shiki shiki-themes github-light github-dark vp-code" tabindex="0"><code><span class="line"><span style="--shiki-light:#6F42C1;--shiki-dark:#B392F0;">find</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> goliac-teams/teams</span><span style="--shiki-light:#005CC5;--shiki-dark:#79B8FF;"> -name</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> &quot;*.yaml&quot;</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> !</span><span style="--shiki-light:#005CC5;--shiki-dark:#79B8FF;"> -name</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> &quot;team.yaml&quot;</span><span style="--shiki-light:#005CC5;--shiki-dark:#79B8FF;"> -print0</span><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;"> |</span><span style="--shiki-light:#6F42C1;--shiki-dark:#B392F0;"> xargs</span><span style="--shiki-light:#005CC5;--shiki-dark:#79B8FF;"> -0</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> rm</span></span></code></pre></div><h3 id="verify" tabindex="-1">Verify <a class="header-anchor" href="#verify" aria-label="Permalink to &quot;Verify&quot;">​</a></h3><p>You can run the</p><div class="language-shell vp-adaptive-theme"><button title="Copy Code" class="copy"></button><span class="lang">shell</span><pre class="shiki shiki-themes github-light github-dark vp-code" tabindex="0"><code><span class="line"><span style="--shiki-light:#6F42C1;--shiki-dark:#B392F0;">goliac</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> verify</span><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;"> &lt;</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;">goliac-team</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> director</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;">y</span><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">&gt;</span></span></code></pre></div><h3 id="publish-the-goliac-teams-repo" tabindex="-1">Publish the goliac teams repo <a class="header-anchor" href="#publish-the-goliac-teams-repo" aria-label="Permalink to &quot;Publish the goliac teams repo&quot;">​</a></h3><p>In Github create a new repository called (for example) <code>goliac-teams</code>.</p><p>And commit the new structure:</p><div class="language- vp-adaptive-theme"><button title="Copy Code" class="copy"></button><span class="lang"></span><pre class="shiki shiki-themes github-light github-dark vp-code" tabindex="0"><code><span class="line"><span>cd goliac-teams</span></span>
<span class="line"><span>git init</span></span>
<span class="line"><span>git add .</span></span>
<span class="line"><span>git commit -m &quot;Initial commit&quot;</span></span>
<span class="line"><span>git push</span></span></code></pre></div><h3 id="adjust-i-e-plan" tabindex="-1">Adjust (i.e. plan) <a class="header-anchor" href="#adjust-i-e-plan" aria-label="Permalink to &quot;Adjust (i.e. plan)&quot;">​</a></h3><p>Run as much as you want, and check what Goliac will do</p><div class="language-shell vp-adaptive-theme"><button title="Copy Code" class="copy"></button><span class="lang">shell</span><pre class="shiki shiki-themes github-light github-dark vp-code" tabindex="0"><code><span class="line"><span style="--shiki-light:#6F42C1;--shiki-dark:#B392F0;">mkdir</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> teams</span></span>
<span class="line"><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">export</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;"> GOLIAC_GITHUB_APP_ID</span><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">=</span><span style="--shiki-light:#005CC5;--shiki-dark:#79B8FF;">355525</span></span>
<span class="line"><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">export</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;"> GOLIAC_GITHUB_APP_PRIVATE_KEY_FILE</span><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">=</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;">goliac-project-app.2023-07-03.private-key.pem</span></span>
<span class="line"><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">export</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;"> GOLIAC_GITHUB_APP_ORGANIZATION</span><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">=</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;">goliac-project</span></span>
<span class="line"><span style="--shiki-light:#6F42C1;--shiki-dark:#B392F0;">./goliac</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> plan</span><span style="--shiki-light:#005CC5;--shiki-dark:#79B8FF;"> --repository</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> https://github.com/goliac-project/goliac-teams</span><span style="--shiki-light:#005CC5;--shiki-dark:#79B8FF;"> --branch</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> main</span></span></code></pre></div><h3 id="apply" tabindex="-1">Apply <a class="header-anchor" href="#apply" aria-label="Permalink to &quot;Apply&quot;">​</a></h3><p>If you are happy with the new structure:</p><div class="language-shell vp-adaptive-theme"><button title="Copy Code" class="copy"></button><span class="lang">shell</span><pre class="shiki shiki-themes github-light github-dark vp-code" tabindex="0"><code><span class="line"><span style="--shiki-light:#6F42C1;--shiki-dark:#B392F0;">mkdir</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> teams</span></span>
<span class="line"><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">export</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;"> GOLIAC_GITHUB_APP_ID</span><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">=</span><span style="--shiki-light:#005CC5;--shiki-dark:#79B8FF;">355525</span></span>
<span class="line"><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">export</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;"> GOLIAC_GITHUB_APP_PRIVATE_KEY_FILE</span><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">=</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;">goliac-project-app.2023-07-03.private-key.pem</span></span>
<span class="line"><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">export</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;"> GOLIAC_GITHUB_APP_ORGANIZATION</span><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">=</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;">goliac-project</span></span>
<span class="line"><span style="--shiki-light:#6F42C1;--shiki-dark:#B392F0;">./goliac</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> apply</span><span style="--shiki-light:#005CC5;--shiki-dark:#79B8FF;"> --repository</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> https://github.com/goliac-project/goliac-teams</span><span style="--shiki-light:#005CC5;--shiki-dark:#79B8FF;"> --branch</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> main</span></span></code></pre></div><h3 id="run-the-server" tabindex="-1">Run the server <a class="header-anchor" href="#run-the-server" aria-label="Permalink to &quot;Run the server&quot;">​</a></h3><p>You can run it locally</p><div class="language-shell vp-adaptive-theme"><button title="Copy Code" class="copy"></button><span class="lang">shell</span><pre class="shiki shiki-themes github-light github-dark vp-code" tabindex="0"><code><span class="line"><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">export</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;"> GOLIAC_GITHUB_APP_ID</span><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">=</span><span style="--shiki-light:#005CC5;--shiki-dark:#79B8FF;">355525</span></span>
<span class="line"><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">export</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;"> GOLIAC_GITHUB_APP_PRIVATE_KEY_FILE</span><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">=</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;">goliac-project-app.2023-07-03.private-key.pem</span></span>
<span class="line"><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">export</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;"> GOLIAC_GITHUB_APP_ORGANIZATION</span><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">=</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;">goliac-project</span></span>
<span class="line"><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">export</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;"> GOLIAC_SERVER_GIT_REPOSITORY</span><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">=</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;">https://github.com/goliac-project/goliac-teams</span></span>
<span class="line"><span style="--shiki-light:#6A737D;--shiki-dark:#6A737D;">#export GOLIAC_SERVER_GIT_BRANCH=main # by default it is main</span></span>
<span class="line"></span>
<span class="line"><span style="--shiki-light:#6F42C1;--shiki-dark:#B392F0;">./goliac</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> serve</span></span></code></pre></div><p>And you can access the dashboard UI at <a href="http://localhost:18000" target="_blank" rel="noreferrer">http://localhost:18000</a></p><h2 id="use-daily" tabindex="-1">Use daily <a class="header-anchor" href="#use-daily" aria-label="Permalink to &quot;Use daily&quot;">​</a></h2><h3 id="add-repositories" tabindex="-1">Add repositories <a class="header-anchor" href="#add-repositories" aria-label="Permalink to &quot;Add repositories&quot;">​</a></h3><p>Now everyone can enroll existing (or new) repositories.</p><p>Let&#39;s imagine you want to control the <code>myrepository</code> repository for the existing team <code>ateam</code>, one of the <code>ateam</code> member (or you) can</p><ul><li>create a new branch into the <code>goiac-teams</code> repository</li><li>add the repository definition</li><li>push and create a PullRequest</li><li>one of the team &quot;owner&quot; or one of the <code>goliac-admin</code> member will be able to approve and merge</li></ul><div class="language-shell vp-adaptive-theme"><button title="Copy Code" class="copy"></button><span class="lang">shell</span><pre class="shiki shiki-themes github-light github-dark vp-code" tabindex="0"><code><span class="line"><span style="--shiki-light:#6F42C1;--shiki-dark:#B392F0;">git</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> checkout</span><span style="--shiki-light:#005CC5;--shiki-dark:#79B8FF;"> -b</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> addingRepository</span></span>
<span class="line"></span>
<span class="line"><span style="--shiki-light:#005CC5;--shiki-dark:#79B8FF;">cd</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> goliac-teams/teams/ateam</span></span>
<span class="line"></span>
<span class="line"><span style="--shiki-light:#6F42C1;--shiki-dark:#B392F0;">cat</span><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;"> &gt;&gt;</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> myrepository.yaml</span><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;"> &lt;&lt;</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> EOF</span></span>
<span class="line"><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;">apiVersion: v1</span></span>
<span class="line"><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;">kind: Repository</span></span>
<span class="line"><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;">name: myrepository</span></span>
<span class="line"><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;">spec:</span></span>
<span class="line"><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;">  public: false</span></span>
<span class="line"><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;">EOF</span></span>
<span class="line"></span>
<span class="line"><span style="--shiki-light:#6F42C1;--shiki-dark:#B392F0;">git</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> add</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> myrepository.yaml</span></span>
<span class="line"><span style="--shiki-light:#6F42C1;--shiki-dark:#B392F0;">git</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> commit</span><span style="--shiki-light:#005CC5;--shiki-dark:#79B8FF;"> -m</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> &#39;adding myrepository under Goliac&#39;</span></span>
<span class="line"><span style="--shiki-light:#6F42C1;--shiki-dark:#B392F0;">git</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> push</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> origin</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;"> addingRepository</span></span></code></pre></div><p>and go to the URL given by Github to create your Pull Request</p>`,46)]))}const g=s(l,[["render",n]]);export{c as __pageData,g as default};
